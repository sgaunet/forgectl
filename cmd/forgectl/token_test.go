package main_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// tokenForge is a GitLab-shaped server carrying the project access token
// lifecycle, so the whole rotation can be driven through the built binary.
type tokenForge struct {
	mu sync.Mutex

	URL string

	tokens    map[int]*fakeToken
	variables map[string]string
	nextID    int

	// Calls records every non-GET request, in order, so a test can assert the
	// create → write → revoke sequence end to end.
	Calls []string
	// FailVariableWrite makes the CI variable write fail, for FR-051.
	FailVariableWrite bool
}

// fakeToken is one project access token the server holds.
type fakeToken struct {
	ID        int
	Name      string
	Active    bool
	Revoked   bool
	ExpiresAt string
}

// newTokenForge starts a compliant project with the given tokens already there.
func newTokenForge(t *testing.T, existing ...*fakeToken) *tokenForge {
	t.Helper()

	f := &tokenForge{
		tokens:    map[int]*fakeToken{},
		variables: map[string]string{},
		nextID:    1,
	}

	for _, tok := range existing {
		f.tokens[tok.ID] = tok
		if tok.ID >= f.nextID {
			f.nextID = tok.ID + 1
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	f.URL = srv.URL

	return f
}

// serve answers one request.
func (f *tokenForge) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if r.Method != http.MethodGet {
		f.Calls = append(f.Calls, r.Method+" "+r.URL.Path)
	}

	w.Header().Set("Content-Type", "application/json")

	switch {
	case r.URL.Path == projectAPI:
		fmt.Fprint(w, `{"id":1,"default_branch":"main"}`)

	case r.URL.Path == projectAPI+"/repository/branches/main":
		fmt.Fprint(w, `{"name":"main"}`)

	case r.URL.Path == projectAPI+"/protected_branches/main":
		fmt.Fprint(w, `{"id":1,"name":"main","allow_force_push":false,
			"push_access_levels":[{"access_level":40}]}`)

	case r.URL.Path == projectAPI+"/protected_tags" && r.Method == http.MethodGet:
		fmt.Fprint(w, `[{"name":"v*"}]`)

	case r.URL.Path == projectAPI+"/access_tokens" && r.Method == http.MethodGet:
		f.listTokens(w)

	case r.URL.Path == projectAPI+"/access_tokens" && r.Method == http.MethodPost:
		f.createToken(w)

	case strings.HasPrefix(r.URL.Path, projectAPI+"/access_tokens/"):
		f.revokeToken(w, r)

	case strings.HasPrefix(r.URL.Path, projectAPI+"/variables/"):
		f.readVariable(w, r)

	case r.URL.Path == projectAPI+"/variables":
		f.writeVariable(w, r)

	default:
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"404 Not found"}`)
	}
}

// listTokens answers the token listing.
func (f *tokenForge) listTokens(w http.ResponseWriter) {
	items := make([]string, 0, len(f.tokens))
	for _, tok := range f.tokens {
		items = append(items, fmt.Sprintf(
			`{"id":%d,"name":%q,"active":%t,"revoked":%t,"expires_at":%q}`,
			tok.ID, tok.Name, tok.Active, tok.Revoked, tok.ExpiresAt))
	}

	fmt.Fprintf(w, "[%s]", strings.Join(items, ","))
}

// createToken creates one and discloses its value, exactly once.
func (f *tokenForge) createToken(w http.ResponseWriter) {
	id := f.nextID
	f.nextID++

	tok := &fakeToken{
		ID: id, Name: "forgectl", Active: true,
		ExpiresAt: time.Now().AddDate(0, 0, 180).Format(time.DateOnly),
	}
	f.tokens[id] = tok

	fmt.Fprintf(w,
		`{"id":%d,"name":"forgectl","active":true,"revoked":false,"expires_at":%q,"token":"glpat-created-%d"}`,
		id, tok.ExpiresAt, id)
}

// revokeToken marks one revoked.
func (f *tokenForge) revokeToken(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimPrefix(r.URL.Path, projectAPI+"/access_tokens/")

	id, err := strconv.Atoi(raw)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)

		return
	}

	if tok, ok := f.tokens[id]; ok {
		tok.Active = false
		tok.Revoked = true
	}

	w.WriteHeader(http.StatusNoContent)
}

// readVariable answers a read of one variable, or stores an update of it.
func (f *tokenForge) readVariable(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, projectAPI+"/variables/")

	// GitLab updates an existing variable with PUT on its own path, so this
	// route has to store as well as read.
	if r.Method == http.MethodPut {
		f.writeVariableNamed(w, r, key)

		return
	}

	value, held := f.variables[key]
	if !held {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"404 Variable Not Found"}`)

		return
	}

	fmt.Fprintf(w, `{"key":%q,"value":%q,"masked":true,"protected":true}`, key, value)
}

// writeVariable stores a create, taking the key from the body.
func (f *tokenForge) writeVariable(w http.ResponseWriter, r *http.Request) {
	body := decodeBody(r)
	key, _ := body["key"].(string)
	f.storeWrite(w, key, body)
}

// writeVariableNamed stores an update, whose key is in the path.
func (f *tokenForge) writeVariableNamed(w http.ResponseWriter, r *http.Request, key string) {
	f.storeWrite(w, key, decodeBody(r))
}

// storeWrite records the value, or fails when the test asked it to.
func (f *tokenForge) storeWrite(w http.ResponseWriter, key string, body map[string]any) {
	if f.FailVariableWrite {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"message":"the platform rejected the write"}`)

		return
	}

	value, _ := body["value"].(string)
	f.variables[key] = value

	fmt.Fprintf(w, `{"key":%q}`, key)
}

// activeTokens counts the live tokens of the given name.
func (f *tokenForge) activeTokens(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	n := 0
	for _, tok := range f.tokens {
		if tok.Name == name && tok.Active && !tok.Revoked {
			n++
		}
	}

	return n
}

// heldVariable returns what the platform holds for a key.
func (f *tokenForge) heldVariable(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.variables[key]
}

// tokenConfig writes a configuration whose profile declares a generated
// variable, the go-release shape.
func tokenConfig(t *testing.T, apiURL string) string {
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
profiles:
  release:
    protected_tags:
      - "v*"
    variables:
      - name: GITLAB_TOKEN
        generator: gitlab-pat
        token_name: forgectl
        expires_in: 180d
        rotate_before: 60d
        masked: true
        protected: true
`, apiURL)

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	return path
}

func TestGeneratedTokenConvergesFromMissing(t *testing.T) {
	// US4 independent test: check reports the token missing, apply creates
	// exactly one and sets the CI variable, and a second check passes reporting
	// the remaining lifetime.
	forge := newTokenForge(t)
	cfg := tokenConfig(t, forge.URL)
	repo := newRepo(t, "main", "git@forge.test:acme/my-tool.git")

	first := forgectl(t, repo, env(cfg), "check", "release")
	if first.code != 3 {
		t.Fatalf("check before apply: exit %d, want 3\nstdout: %s", first.code, first.stdout)
	}
	if !strings.Contains(first.stdout, "token missing") {
		t.Errorf("the report does not say the token is missing:\n%s", first.stdout)
	}

	applied := forgectl(t, repo, env(cfg), "apply", "release", "--yes")
	if applied.code != 0 {
		t.Fatalf("apply: exit %d\nstdout: %s\nstderr: %s",
			applied.code, applied.stdout, applied.stderr)
	}

	// FR-049: exactly one active token, and the CI variable holds it.
	if n := forge.activeTokens("forgectl"); n != 1 {
		t.Errorf("active tokens = %d, want exactly 1", n)
	}
	if got := forge.heldVariable("GITLAB_TOKEN"); !strings.HasPrefix(got, "glpat-created-") {
		t.Errorf("the CI variable holds %q, want the created token", got)
	}

	second := forgectl(t, repo, env(cfg), "check", "release", "--output=json")
	if second.code != 0 {
		t.Fatalf("check after apply: exit %d, want 0\nstdout: %s", second.code, second.stdout)
	}

	// FR-055: the entry carries the generator name, the expiry, and the days
	// remaining.
	var doc struct {
		Checks []struct {
			ID           string `json:"id"`
			Generator    string `json:"generator"`
			ExpiresAt    string `json:"expires_at"`
			RotateInDays *int   `json:"rotate_in_days"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(second.stdout), &doc); err != nil {
		t.Fatalf("stdout does not parse: %v\n%s", err, second.stdout)
	}

	var found bool
	for _, c := range doc.Checks {
		if c.Generator == "" {
			continue
		}
		found = true

		if c.ID != "vars:GITLAB_TOKEN" {
			t.Errorf("id = %q", c.ID)
		}
		if c.ExpiresAt == "" {
			t.Error("the entry carries no expiry date")
		}
		if c.RotateInDays == nil || *c.RotateInDays < 100 {
			t.Errorf("rotate_in_days = %v, want roughly 120", c.RotateInDays)
		}
	}
	if !found {
		t.Errorf("no generated variable reached the document:\n%s", second.stdout)
	}
}

func TestForceRotateReplacesAHealthyToken(t *testing.T) {
	// FR-053: --force-rotate rotates even when no drift was found, leaving
	// exactly one active token.
	healthy := &fakeToken{
		ID: 1, Name: "forgectl", Active: true,
		ExpiresAt: time.Now().AddDate(0, 0, 170).Format(time.DateOnly),
	}

	forge := newTokenForge(t, healthy)
	forge.variables["GITLAB_TOKEN"] = "glpat-existing"

	cfg := tokenConfig(t, forge.URL)
	repo := newRepo(t, "main", "git@forge.test:acme/my-tool.git")

	// Without the flag there is nothing to do.
	quiet := forgectl(t, repo, env(cfg), "apply", "release", "--yes")
	if quiet.code != 0 {
		t.Fatalf("apply on a healthy token: exit %d\nstderr: %s", quiet.code, quiet.stderr)
	}
	if !strings.Contains(quiet.stderr, "nothing to do") {
		t.Errorf("a healthy token still planned work:\n%s", quiet.stderr)
	}

	forced := forgectl(t, repo, env(cfg), "apply", "release", "--yes", "--force-rotate")
	if forced.code != 0 {
		t.Fatalf("forced rotation: exit %d\nstdout: %s\nstderr: %s",
			forced.code, forced.stdout, forced.stderr)
	}

	if n := forge.activeTokens("forgectl"); n != 1 {
		t.Errorf("active tokens = %d, want exactly 1 after rotation", n)
	}
	if got := forge.heldVariable("GITLAB_TOKEN"); got == "glpat-existing" {
		t.Error("the CI variable still holds the replaced token")
	}
}

func TestAStrandedTokenIsReported(t *testing.T) {
	// FR-051: a token created while the variable write fails cannot be
	// recovered, and the report must say so.
	forge := newTokenForge(t)
	forge.FailVariableWrite = true

	cfg := tokenConfig(t, forge.URL)
	repo := newRepo(t, "main", "git@forge.test:acme/my-tool.git")

	got := forgectl(t, repo, env(cfg), "apply", "release", "--yes")

	if got.code != 1 {
		t.Fatalf("exit = %d, want 1\nstdout: %s\nstderr: %s", got.code, got.stdout, got.stderr)
	}

	combined := got.stdout + got.stderr
	for _, want := range []string{"remains active", "rerun"} {
		if !strings.Contains(combined, want) {
			t.Errorf("the report does not say %q:\n%s", want, combined)
		}
	}

	// The token really is still there, which is what the message warns about.
	if n := forge.activeTokens("forgectl"); n != 1 {
		t.Errorf("active tokens = %d, want the stranded one to still exist", n)
	}
}

func TestAGeneratedVariableOnGitHubSkipsWithoutFailing(t *testing.T) {
	// FR-029: on a GitHub instance the generated variable is skipped with a
	// warning, and the run does not fail.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/v3/repos/acme/my-tool":
			fmt.Fprint(w, `{"default_branch":"main"}`)
		case "/api/v3/repos/acme/my-tool/branches/main":
			fmt.Fprint(w, `{"name":"main"}`)
		case "/api/v3/repos/acme/my-tool/rulesets":
			fmt.Fprint(w, `[{"id":1,"name":"forgectl","target":"branch","enforcement":"active"}]`)
		case "/api/v3/repos/acme/my-tool/rulesets/1":
			fmt.Fprint(w, `{"id":1,"name":"forgectl","target":"branch","source":"acme/my-tool",
				"enforcement":"active",
				"conditions":{"ref_name":{"include":["refs/heads/main","refs/tags/v*"],"exclude":[]}},
				"rules":[{"type":"deletion"},{"type":"non_fast_forward"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	defer srv.Close()

	body := fmt.Sprintf(`
settings:
  default_branch: main
instances:
  - name: gh-test
    host: forge.test
    platform: github
    api_url: %s/
    token_env: FORGE_TEST_TOKEN
profiles:
  release:
    variables:
      - name: GITLAB_TOKEN
        generator: gitlab-pat
`, srv.URL)

	cfg := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	repo := newRepo(t, "main", "git@forge.test:acme/my-tool.git")

	got := forgectl(t, repo, env(cfg), "check", "release", "--output=json")

	if got.code != 0 {
		t.Fatalf("exit = %d, want 0: a skip is not drift\nstdout: %s\nstderr: %s",
			got.code, got.stdout, got.stderr)
	}

	var doc struct {
		Checks []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Reason string `json:"reason"`
		} `json:"checks"`
		Summary struct {
			Fail int `json:"fail"`
			Skip int `json:"skip"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(got.stdout), &doc); err != nil {
		t.Fatalf("stdout does not parse: %v\n%s", err, got.stdout)
	}

	if doc.Summary.Fail != 0 {
		t.Errorf("fail count = %d, want 0", doc.Summary.Fail)
	}

	var found bool
	for _, c := range doc.Checks {
		if c.ID != "vars:GITLAB_TOKEN" {
			continue
		}
		found = true

		if c.Status != "skip" {
			t.Errorf("status = %s, want skip", c.Status)
		}
		if !strings.Contains(c.Reason, "project access token") {
			t.Errorf("reason = %q, want it to explain what the platform lacks", c.Reason)
		}
	}
	if !found {
		t.Errorf("the generated variable produced no check:\n%s", got.stdout)
	}
}
