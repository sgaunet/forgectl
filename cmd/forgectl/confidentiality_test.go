package main_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// sentinels are recognisable values seeded through every path a value can take.
// SC-003 requires that the number of values disclosed across every output path
// is zero, and the only way to assert "zero" is to look for values you planted.
var sentinels = map[string]string{
	"galaxy":  "SENTINEL-galaxy-3f9a2b7c4d1e",
	"varfile": "SENTINEL-varfile-8e2d5a1c9b3f",
	"inline":  "SENTINEL-inline-6b4c8f0a2d7e",
}

// variableForge is a GitLab-shaped server that accepts variable writes and
// remembers the values it was given, so a test can confirm the value really did
// reach the platform — the one place it is allowed to be.
type variableForge struct {
	mu sync.Mutex

	URL    string
	Values map[string]string
}

// newVariableForge starts a compliant project that accepts variable writes.
func newVariableForge(t *testing.T) *variableForge {
	t.Helper()

	f := &variableForge{Values: map[string]string{}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == projectAPI:
			fmt.Fprint(w, `{"id":1,"default_branch":"main"}`)

		case r.URL.Path == projectAPI+"/repository/branches/main":
			fmt.Fprint(w, `{"name":"main"}`)

		case r.URL.Path == projectAPI+"/protected_branches/main":
			fmt.Fprint(w, `{"id":1,"name":"main","allow_force_push":false,
				"push_access_levels":[{"access_level":40}]}`)

		case r.URL.Path == projectAPI+"/protected_tags":
			fmt.Fprint(w, `[]`)

		case strings.HasPrefix(r.URL.Path, projectAPI+"/variables/"):
			f.serveVariable(w, r)

		case r.URL.Path == projectAPI+"/variables" && r.Method == http.MethodPost:
			f.acceptWrite(w, r)

		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"404 Not found"}`)
		}
	}))
	t.Cleanup(srv.Close)

	f.URL = srv.URL

	return f
}

// serveVariable answers a read of one variable.
func (f *variableForge) serveVariable(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, projectAPI+"/variables/")

	value, held := f.Values[key]
	if !held {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"404 Variable Not Found"}`)

		return
	}

	fmt.Fprintf(w, `{"key":%q,"value":%q,"masked":true,"protected":false}`, key, value)
}

// acceptWrite stores what a create-variable request carried.
func (f *variableForge) acceptWrite(w http.ResponseWriter, r *http.Request) {
	body := decodeBody(r)

	key, _ := body["key"].(string)
	value, _ := body["value"].(string)
	f.Values[key] = value

	fmt.Fprintf(w, `{"key":%q}`, key)
}

// held reports whether the platform received a value.
func (f *variableForge) held(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.Values[key]
}

// confidentialityConfig writes a configuration seeding two sentinels: one in
// the shared value store, one as an inline literal.
func confidentialityConfig(t *testing.T, apiURL string) string {
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
values:
  galaxy_api_token: %s
profiles:
  confidential:
    variables:
      - name: GALAXY_API_TOKEN
        value_ref: galaxy_api_token
        masked: true
      - name: INLINE_TOKEN
        value: %s
        masked: true
`, apiURL, sentinels["galaxy"], sentinels["inline"])

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	return path
}

func TestNoValueIsEverDisclosed(t *testing.T) {
	// SC-003 and FR-054: across every output path — human-readable,
	// machine-readable, logs, and error messages — the number of values
	// disclosed is zero.
	forge := newVariableForge(t)
	cfg := confidentialityConfig(t, forge.URL)
	repo := newRepo(t, "main", "git@forge.test:acme/my-tool.git")

	// A third sentinel arrives through --var-file, the highest-precedence
	// source. The file must be outside the working copy, or FR-056 refuses it.
	varFile := filepath.Join(t.TempDir(), "vars.yaml")
	if err := os.WriteFile(varFile,
		[]byte("values:\n  GALAXY_API_TOKEN: "+sentinels["varfile"]+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	runs := []struct {
		name string
		args []string
	}{
		{name: "check text", args: []string{"check", "confidential"}},
		{name: "check json", args: []string{"check", "confidential", "--output=json"}},
		{name: "check verbose", args: []string{"check", "confidential", "--verbose"}},
		{name: "profiles show", args: []string{"profiles", "show", "confidential"}},
		{
			name: "apply with an override file",
			args: []string{"apply", "confidential", "--yes", "--var-file", varFile, "--verbose"},
		},
		{
			name: "apply json",
			args: []string{"apply", "confidential", "--yes", "--var-file", varFile, "--output=json"},
		},
	}

	for _, run := range runs {
		t.Run(run.name, func(t *testing.T) {
			got := forgectl(t, repo, env(cfg), run.args...)

			for label, value := range sentinels {
				if strings.Contains(got.stdout, value) {
					t.Errorf("the %s value appears on stdout:\n%s", label, got.stdout)
				}
				if strings.Contains(got.stderr, value) {
					t.Errorf("the %s value appears on stderr:\n%s", label, got.stderr)
				}
			}
		})
	}

	// And the value DID reach the platform, which is the only place it belongs.
	// A test that merely found no value everywhere would also pass if forgectl
	// had written nothing at all.
	if got := forge.held("GALAXY_API_TOKEN"); got != sentinels["varfile"] {
		t.Errorf("the platform holds %q, want the override file's value: "+
			"the absence of leaks must not come from the absence of writes", got)
	}
	if got := forge.held("INLINE_TOKEN"); got != sentinels["inline"] {
		t.Errorf("the platform holds %q for INLINE_TOKEN, want the configured value", got)
	}
}

func TestNoValueAppearsInAnErrorMessage(t *testing.T) {
	// FR-054 covers error messages too, which is the path most easily
	// forgotten: a failing write is exactly when a developer reaches for
	// "%v" on the whole request.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case projectAPI:
			fmt.Fprint(w, `{"id":1,"default_branch":"main"}`)
		case projectAPI + "/repository/branches/main":
			fmt.Fprint(w, `{"name":"main"}`)
		case projectAPI + "/protected_branches/main":
			fmt.Fprint(w, `{"id":1,"name":"main","allow_force_push":false,
				"push_access_levels":[{"access_level":40}]}`)
		case projectAPI + "/protected_tags":
			fmt.Fprint(w, `[]`)
		default:
			// Every variable call fails, so the value is on the failing path.
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"message":"the platform said no"}`)
		}
	}))
	defer srv.Close()

	cfg := confidentialityConfig(t, srv.URL)
	repo := newRepo(t, "main", "git@forge.test:acme/my-tool.git")

	got := forgectl(t, repo, env(cfg), "apply", "confidential", "--yes", "--verbose")

	if got.code == 0 {
		t.Fatal("apply reported success though every variable write failed")
	}

	for label, value := range sentinels {
		if strings.Contains(got.stdout+got.stderr, value) {
			t.Errorf("the %s value appears in the failure output:\nstdout: %s\nstderr: %s",
				label, got.stdout, got.stderr)
		}
	}
}

func TestVariablesConvergeFromMissingToCompliant(t *testing.T) {
	// US3 independent test: with a profile declaring variables and a
	// configuration providing their values, check reports them missing, apply
	// creates them, and a second check passes.
	forge := newVariableForge(t)
	cfg := confidentialityConfig(t, forge.URL)
	repo := newRepo(t, "main", "git@forge.test:acme/my-tool.git")

	first := forgectl(t, repo, env(cfg), "check", "confidential")
	if first.code != 3 {
		t.Fatalf("check before apply: exit %d, want 3\nstdout: %s", first.code, first.stdout)
	}
	for _, name := range []string{"vars:GALAXY_API_TOKEN", "vars:INLINE_TOKEN"} {
		if !strings.Contains(first.stdout, name) {
			t.Errorf("the report does not mention %s:\n%s", name, first.stdout)
		}
	}
	if !strings.Contains(first.stdout, "missing") {
		t.Errorf("the report does not say the variables are missing:\n%s", first.stdout)
	}

	applied := forgectl(t, repo, env(cfg), "apply", "confidential", "--yes")
	if applied.code != 0 {
		t.Fatalf("apply: exit %d\nstdout: %s\nstderr: %s",
			applied.code, applied.stdout, applied.stderr)
	}

	second := forgectl(t, repo, env(cfg), "check", "confidential")
	if second.code != 0 {
		t.Fatalf("check after apply: exit %d, want 0\nstdout: %s", second.code, second.stdout)
	}
}

func TestApplyFailsListingEveryMissingValue(t *testing.T) {
	// FR-044: with no source for a value and no terminal, apply fails listing
	// every missing name BEFORE making any change.
	forge := newVariableForge(t)

	body := fmt.Sprintf(`
settings:
  default_branch: main
instances:
  - name: test-forge
    host: forge.test
    platform: gitlab
    api_url: %s/api/v4
    token_env: FORGE_TEST_TOKEN
values:
  declared_but_blank: ""
profiles:
  incomplete:
    variables:
      - name: ALPHA
        value_ref: declared_but_blank
      - name: BETA
        value_ref: declared_but_blank
`, forge.URL)

	cfg := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	repo := newRepo(t, "main", "git@forge.test:acme/my-tool.git")

	got := forgectl(t, repo, env(cfg), "apply", "incomplete", "--yes")

	if got.code == 0 {
		t.Fatalf("apply succeeded with no value for either variable\nstdout: %s", got.stdout)
	}

	for _, name := range []string{"ALPHA", "BETA"} {
		if !strings.Contains(got.stderr, name) {
			t.Errorf("stderr does not name %s; every missing value must be listed:\n%s",
				name, got.stderr)
		}
	}

	// And nothing was written: the failure came before the first change.
	if len(forge.Values) != 0 {
		t.Errorf("apply wrote %v before discovering the missing values", forge.Values)
	}
}

func TestAVarFileInsideTheRepositoryIsRefusedBeforeAnyPlatformCall(t *testing.T) {
	// FR-056 and FR-057: the refusal is raised at load time, before any
	// platform call and before any value is read from the file. Opening the
	// forge first would turn the refusal into a network error, which is exactly
	// what the quickstart walk-through caught.
	forge := newVariableForge(t)
	cfg := confidentialityConfig(t, forge.URL)
	repo := newRepo(t, "main", "git@forge.test:acme/my-tool.git")

	// The file lies inside the working copy and git does not ignore it.
	varFile := filepath.Join(repo, "vars.yaml")
	if err := os.WriteFile(varFile,
		[]byte("values:\n  GALAXY_API_TOKEN: "+sentinels["varfile"]+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got := forgectl(t, repo, env(cfg),
		"apply", "confidential", "--yes", "--var-file", varFile)

	if got.code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout: %s\nstderr: %s", got.code, got.stdout, got.stderr)
	}

	// The message names the file and the ignore entry that resolves it.
	for _, want := range []string{"vars.yaml", ".gitignore", "no bypass"} {
		if !strings.Contains(got.stderr, want) {
			t.Errorf("stderr does not mention %q:\n%s", want, got.stderr)
		}
	}

	// And nothing was written, because nothing was even asked of the platform.
	if len(forge.Values) != 0 {
		t.Errorf("the platform was written to despite the refusal: %v", forge.Values)
	}
}

func TestTheInRepositoryRefusalHasNoBypass(t *testing.T) {
	// FR-056: --allow-insecure-config lifts the permission check of FR-007 and
	// nothing else.
	forge := newVariableForge(t)
	cfg := confidentialityConfig(t, forge.URL)
	repo := newRepo(t, "main", "git@forge.test:acme/my-tool.git")

	varFile := filepath.Join(repo, "vars.yaml")
	if err := os.WriteFile(varFile, []byte("values:\n  GALAXY_API_TOKEN: v\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got := forgectl(t, repo, env(cfg),
		"apply", "confidential", "--yes", "--var-file", varFile, "--allow-insecure-config")

	if got.code != 2 {
		t.Errorf("--allow-insecure-config bypassed the refusal: exit %d\nstderr: %s",
			got.code, got.stderr)
	}
}

func TestAnIgnoredVarFileIsAcceptedWithoutWarning(t *testing.T) {
	// The same file, ignored by git, is accepted: a value that cannot be
	// committed is not in the repository.
	forge := newVariableForge(t)
	cfg := confidentialityConfig(t, forge.URL)
	repo := newRepo(t, "main", "git@forge.test:acme/my-tool.git")

	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("vars.yaml\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	varFile := filepath.Join(repo, "vars.yaml")
	if err := os.WriteFile(varFile,
		[]byte("values:\n  GALAXY_API_TOKEN: "+sentinels["varfile"]+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got := forgectl(t, repo, env(cfg),
		"apply", "confidential", "--yes", "--var-file", varFile)

	if got.code != 0 {
		t.Fatalf("an ignored values file was refused: exit %d\nstderr: %s", got.code, got.stderr)
	}
	if got := forge.held("GALAXY_API_TOKEN"); got != sentinels["varfile"] {
		t.Errorf("the platform holds %q, want the override file's value", got)
	}
}
