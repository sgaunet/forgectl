package main_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeForge is an httptest GitLab standing in for a real instance, so the whole
// binary can be driven end to end with no network.
type fakeForge struct {
	URL string
	// Calls records every request as it arrived on the wire — RequestURI, not
	// the decoded path — so a test can assert how the project id was encoded.
	Calls []string
	// Mutations records every request that was not a GET.
	Mutations []string
}

// newFakeForge starts a GitLab-shaped server describing one project.
func newFakeForge(t *testing.T, defaultBranch string, protectedBranches map[string]bool) *fakeForge {
	t.Helper()

	f := &fakeForge{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.Calls = append(f.Calls, r.Method+" "+r.RequestURI)
		if r.Method != http.MethodGet {
			f.Mutations = append(f.Mutations, r.Method+" "+r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")

		const project = "/api/v4/projects/acme/my-tool"

		switch {
		case r.URL.Path == project:
			fmt.Fprintf(w, `{"id":1,"default_branch":%q}`, defaultBranch)

		case strings.HasPrefix(r.URL.Path, project+"/repository/branches/"):
			name := strings.TrimPrefix(r.URL.Path, project+"/repository/branches/")
			if name != defaultBranch {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"message":"404 Branch Not Found"}`)

				return
			}
			fmt.Fprintf(w, `{"name":%q}`, name)

		case strings.HasPrefix(r.URL.Path, project+"/protected_branches/"):
			name := strings.TrimPrefix(r.URL.Path, project+"/protected_branches/")
			if !protectedBranches[name] {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"message":"404 Not found"}`)

				return
			}
			fmt.Fprintf(w,
				`{"id":1,"name":%q,"allow_force_push":false,
				  "push_access_levels":[{"access_level":40}]}`, name)

		case r.URL.Path == project+"/protected_tags":
			fmt.Fprint(w, `[]`)

		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"404 Not found"}`)
		}
	}))
	t.Cleanup(srv.Close)

	f.URL = srv.URL

	return f
}

// configFor writes a configuration declaring an instance pointing at the fake
// forge, and returns its path.
func configFor(t *testing.T, apiURL string) string {
	t.Helper()

	body := fmt.Sprintf(`
settings:
  default_branch: main
instances:
  - name: test-forge
    host: forge.test
    platform: gitlab
    api_url: %s/api/v4
    token_env: FORGE_TEST_TOKEN
`, apiURL)

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	return path
}

// env builds the environment a run against the fake forge needs.
func env(cfg string) []string {
	return []string{"FORGE_TEST_TOKEN=glpat-test", "FORGECTL_CONFIG=" + cfg}
}

func TestCheckExitsThreeOnDrift(t *testing.T) {
	// US1 independent test: a repository whose default branch is master reports
	// the branch drift and signals it through the exit status.
	forge := newFakeForge(t, "master", nil)
	cfg := configFor(t, forge.URL)
	repo := newRepo(t, "master", "git@forge.test:acme/my-tool.git")

	got := forgectl(t, repo, env(cfg), "check")

	if got.code != 3 {
		t.Fatalf("exit = %d, want 3\nstdout: %s\nstderr: %s", got.code, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stdout, "expected main, found master") {
		t.Errorf("the report does not name the drift:\n%s", got.stdout)
	}

	// FR-031: check modified nothing.
	if len(forge.Mutations) != 0 {
		t.Errorf("check made mutating calls: %v", forge.Mutations)
	}
}

func TestCheckExitsZeroWhenCompliant(t *testing.T) {
	forge := newFakeForge(t, "main", map[string]bool{"main": true})
	cfg := configFor(t, forge.URL)
	repo := newRepo(t, "main", "git@forge.test:acme/my-tool.git")

	got := forgectl(t, repo, env(cfg), "check")

	if got.code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout: %s\nstderr: %s", got.code, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stdout, "0 failed") {
		t.Errorf("the summary does not report zero failures:\n%s", got.stdout)
	}
}

func TestCheckSkipsProtectionWhenTheBranchIsAbsent(t *testing.T) {
	// US1 acceptance scenario 3: the skip is stated with its reason and counted
	// separately from a failure.
	forge := newFakeForge(t, "master", nil)
	cfg := configFor(t, forge.URL)
	repo := newRepo(t, "master", "git@forge.test:acme/my-tool.git")

	got := forgectl(t, repo, env(cfg), "check", "--output=json")

	var doc struct {
		Checks []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Reason string `json:"reason"`
		} `json:"checks"`
		Summary struct {
			Pass int `json:"pass"`
			Fail int `json:"fail"`
			Skip int `json:"skip"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(got.stdout), &doc); err != nil {
		t.Fatalf("stdout does not parse: %v\n%s", err, got.stdout)
	}

	if doc.Summary.Skip != 1 {
		t.Errorf("skip count = %d, want 1", doc.Summary.Skip)
	}
	if doc.Summary.Fail != 1 {
		t.Errorf("fail count = %d, want 1 (the branch drift alone)", doc.Summary.Fail)
	}

	for _, c := range doc.Checks {
		if c.ID == "protection" && (c.Status != "skip" || c.Reason == "") {
			t.Errorf("protection = %s with reason %q, want a skip carrying its reason", c.Status, c.Reason)
		}
	}
}

func TestCheckWarnsWhenNoProfileIsSelected(t *testing.T) {
	// FR-019: the warning belongs on stderr, and must not pollute the document.
	forge := newFakeForge(t, "main", map[string]bool{"main": true})
	cfg := configFor(t, forge.URL)
	repo := newRepo(t, "main", "git@forge.test:acme/my-tool.git")

	got := forgectl(t, repo, env(cfg), "check", "--output=json")

	if !strings.Contains(got.stderr, "CI variables were not checked") {
		t.Errorf("stderr carries no warning about unchecked variables:\n%s", got.stderr)
	}

	var doc map[string]any
	if err := json.Unmarshal([]byte(got.stdout), &doc); err != nil {
		t.Fatalf("the warning corrupted the document: %v\n%s", err, got.stdout)
	}

	// The warning is repeated in the document, so a JSON consumer sees it
	// without reading two streams.
	warnings, ok := doc["warnings"].([]any)
	if !ok || len(warnings) == 0 {
		t.Error("the document carries no warnings array")
	}
}

func TestCheckReportsARuntimeFailureAsExitOne(t *testing.T) {
	// CLI-002: a platform failure is not drift. It exits 1, whatever the state
	// of the repository.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"boom"}`))
	}))
	defer srv.Close()

	cfg := configFor(t, srv.URL)
	repo := newRepo(t, "master", "git@forge.test:acme/my-tool.git")

	got := forgectl(t, repo, env(cfg), "check")

	if got.code != 1 {
		t.Errorf("exit = %d, want 1\nstdout: %s\nstderr: %s", got.code, got.stdout, got.stderr)
	}
}

func TestProjectIDIsURLEncoded(t *testing.T) {
	// The GitLab project id is owner/repo, URL-encoded on the wire.
	forge := newFakeForge(t, "main", map[string]bool{"main": true})
	cfg := configFor(t, forge.URL)
	repo := newRepo(t, "main", "git@forge.test:acme/my-tool.git")

	if got := forgectl(t, repo, env(cfg), "check"); got.code != 0 {
		t.Fatalf("exit = %d\nstderr: %s", got.code, got.stderr)
	}

	want := "/api/v4/projects/" + url.PathEscape("acme/my-tool")
	for _, call := range forge.Calls {
		if strings.Contains(call, want) {
			return
		}
	}
	t.Errorf("no call used the encoded project id; calls were %v", forge.Calls)
}

// decodeBody parses a request body as JSON, for tests that need to inspect what
// forgectl sent.
func decodeBody(r *http.Request) map[string]any {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return nil
	}

	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil
	}

	return body
}
