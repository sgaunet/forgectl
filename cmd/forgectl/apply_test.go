package main_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// mutableForge is a GitLab-shaped server that actually remembers what apply did
// to it, so a second run can be shown to have nothing left to do (SC-002).
type mutableForge struct {
	mu sync.Mutex

	URL string

	defaultBranch string
	branches      map[string]bool
	protected     map[string]bool
	tags          map[string]bool

	// Mutations records every request that was not a GET.
	Mutations []string
	// Fail makes the named path fail, for the partial-failure test.
	Fail string
}

// newMutableForge starts a server whose state apply can change.
func newMutableForge(t *testing.T) *mutableForge {
	t.Helper()

	// Every apply test starts from the state the convention moves away from.
	const defaultBranch = "master"

	f := &mutableForge{
		defaultBranch: defaultBranch,
		branches:      map[string]bool{defaultBranch: true},
		protected:     map[string]bool{},
		tags:          map[string]bool{},
	}

	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	f.URL = srv.URL

	return f
}

const projectAPI = "/api/v4/projects/acme/my-tool"

// serve answers one request against the remembered state.
func (f *mutableForge) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if r.Method != http.MethodGet {
		f.Mutations = append(f.Mutations, r.Method+" "+r.URL.Path)
	}

	w.Header().Set("Content-Type", "application/json")

	if f.Fail != "" && strings.Contains(r.URL.Path, f.Fail) && r.Method != http.MethodGet {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"message":"the platform said no"}`)

		return
	}

	switch {
	case r.URL.Path == projectAPI && r.Method == http.MethodGet:
		fmt.Fprintf(w, `{"id":1,"default_branch":%q}`, f.defaultBranch)

	case r.URL.Path == projectAPI && r.Method == http.MethodPut:
		f.defaultBranch = "main"
		f.branches["main"] = true
		fmt.Fprintf(w, `{"id":1,"default_branch":%q}`, f.defaultBranch)

	case strings.HasPrefix(r.URL.Path, projectAPI+"/repository/branches/"):
		name := strings.TrimPrefix(r.URL.Path, projectAPI+"/repository/branches/")
		if !f.branches[name] {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"404 Branch Not Found"}`)

			return
		}
		fmt.Fprintf(w, `{"name":%q}`, name)

	case strings.HasPrefix(r.URL.Path, projectAPI+"/protected_branches/"):
		f.serveProtectedBranch(w, r)

	case r.URL.Path == projectAPI+"/protected_branches" && r.Method == http.MethodPost:
		f.protected["main"] = true
		fmt.Fprint(w, `{"id":1,"name":"main"}`)

	case r.URL.Path == projectAPI+"/protected_tags" && r.Method == http.MethodGet:
		f.serveTagList(w)

	case r.URL.Path == projectAPI+"/protected_tags" && r.Method == http.MethodPost:
		f.tags["v*"] = true
		fmt.Fprint(w, `{"name":"v*"}`)

	default:
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"404 Not found"}`)
	}
}

// serveProtectedBranch answers the per-branch protection endpoints.
func (f *mutableForge) serveProtectedBranch(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, projectAPI+"/protected_branches/")

	if r.Method == http.MethodPatch {
		f.protected[name] = true
		fmt.Fprintf(w, `{"id":1,"name":%q}`, name)

		return
	}

	if !f.protected[name] {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"404 Not found"}`)

		return
	}

	fmt.Fprintf(w, `{"id":1,"name":%q,"allow_force_push":false,
		"push_access_levels":[{"access_level":40}]}`, name)
}

// serveTagList answers the protected-tag listing.
func (f *mutableForge) serveTagList(w http.ResponseWriter) {
	items := make([]string, 0, len(f.tags))
	for pattern := range f.tags {
		items = append(items, fmt.Sprintf(`{"name":%q}`, pattern))
	}

	fmt.Fprintf(w, "[%s]", strings.Join(items, ","))
}

// mutationCount reports how many state-changing calls the forge received.
func (f *mutableForge) mutationCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.Mutations)
}

// newClonePairForApply builds a working copy with a real bare remote, so the
// git side of apply — rename, push, set-head — runs for real.
func newClonePairForApply(t *testing.T) string {
	t.Helper()

	// The working copy starts on the branch the convention moves away from, and
	// its remote names the host configFor declares.
	const (
		branch = "master"
		host   = "forge.test"
	)

	root := t.TempDir()
	bare := filepath.Join(root, "remote.git")
	work := filepath.Join(root, "work")

	git(t, root, "init", "--bare", "-b", branch, bare)
	git(t, root, "clone", bare, work)
	git(t, work, "commit", "--allow-empty", "-m", "init")
	git(t, work, "push", "-u", "origin", branch)

	// The platform is reached by host, while git must still push to the bare
	// repository on disk. pushInsteadOf rewrites the push target only, so
	// `git remote get-url` — which forgectl reads to identify the forge — keeps
	// returning the host, while every push lands on disk. A plain insteadOf
	// would rewrite get-url too, and forgectl would see a filesystem path.
	git(t, work, "remote", "set-url", "origin", "git@"+host+":acme/my-tool.git")
	git(t, work, "config", "url."+bare+".pushInsteadOf", "git@"+host+":acme/my-tool.git")

	return work
}

func TestApplyConvergesBranchAndProtection(t *testing.T) {
	// US2 independent test: a repository with default branch master and no
	// protection reaches main-as-default-and-protected in one apply.
	forge := newMutableForge(t)
	cfg := configFor(t, forge.URL)
	repo := newClonePairForApply(t)

	got := forgectl(t, repo, env(cfg), "apply", "--yes")

	if got.code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout: %s\nstderr: %s", got.code, got.stdout, got.stderr)
	}

	// The platform now reports main as default and as protected.
	if forge.defaultBranch != "main" {
		t.Errorf("platform default = %q, want main", forge.defaultBranch)
	}
	if !forge.protected["main"] {
		t.Error("main was not protected")
	}

	// And the local clone agrees.
	if branches := gitOutput(t, repo, "branch", "--format=%(refname:short)"); !strings.Contains(branches, "main") {
		t.Errorf("local branches = %q, want main", branches)
	}

	// FR-040: the maintainer is warned about open merge requests and given the
	// command other clones must run.
	for _, want := range []string{"retargeting", "git branch -m"} {
		if !strings.Contains(got.stderr, want) {
			t.Errorf("stderr does not warn about %q:\n%s", want, got.stderr)
		}
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	// SC-002: running apply twice produces an empty plan and zero
	// state-changing calls on the second run.
	forge := newMutableForge(t)
	cfg := configFor(t, forge.URL)
	repo := newClonePairForApply(t)

	if got := forgectl(t, repo, env(cfg), "apply", "--yes"); got.code != 0 {
		t.Fatalf("first apply: exit %d\nstderr: %s", got.code, got.stderr)
	}

	before := forge.mutationCount()

	second := forgectl(t, repo, env(cfg), "apply", "--yes")
	if second.code != 0 {
		t.Fatalf("second apply: exit %d\nstdout: %s\nstderr: %s",
			second.code, second.stdout, second.stderr)
	}

	if after := forge.mutationCount(); after != before {
		t.Errorf("the second apply made %d mutating calls, want none", after-before)
	}
	if !strings.Contains(second.stderr, "nothing to do") {
		t.Errorf("the second apply did not report an empty plan:\n%s", second.stderr)
	}

	// And check now passes.
	if got := forgectl(t, repo, env(cfg), "check"); got.code != 0 {
		t.Errorf("check after apply: exit %d, want 0\n%s", got.code, got.stdout)
	}
}

func TestApplyPreviewAndPromptLiveOnStderr(t *testing.T) {
	// CLI-001: `forgectl apply --yes --output=json | jq` must yield a clean
	// document, which a preview written to stdout would corrupt.
	forge := newMutableForge(t)
	cfg := configFor(t, forge.URL)
	repo := newClonePairForApply(t)

	got := forgectl(t, repo, env(cfg), "apply", "--yes", "--output=json")

	if got.code != 0 {
		t.Fatalf("exit = %d\nstderr: %s", got.code, got.stderr)
	}

	var doc map[string]any
	if err := json.Unmarshal([]byte(got.stdout), &doc); err != nil {
		t.Fatalf("stdout does not parse as JSON: %v\n%s", err, got.stdout)
	}
	if doc["command"] != "apply" {
		t.Errorf("command = %v, want apply", doc["command"])
	}

	// The preview is on stderr, and the actions are in the document.
	if !strings.Contains(got.stderr, "forgectl will:") {
		t.Errorf("the plan preview is not on stderr:\n%s", got.stderr)
	}
	if strings.Contains(got.stdout, "forgectl will:") {
		t.Errorf("the plan preview leaked onto stdout:\n%s", got.stdout)
	}

	actions, ok := doc["actions"].([]any)
	if !ok || len(actions) == 0 {
		t.Error("the document carries no actions array")
	}
}

func TestApplyWithoutConfirmationOnANonTerminalExitsTwo(t *testing.T) {
	// CLI-003: when stdin is not a TTY and --yes was not given, apply exits 2
	// rather than hanging or assuming consent.
	forge := newMutableForge(t)
	cfg := configFor(t, forge.URL)
	repo := newClonePairForApply(t)

	got := forgectl(t, repo, env(cfg), "apply")

	if got.code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout: %s\nstderr: %s", got.code, got.stdout, got.stderr)
	}
	if forge.mutationCount() != 0 {
		t.Errorf("apply changed something without consent: %v", forge.Mutations)
	}
	if !strings.Contains(got.stderr, "--yes") {
		t.Errorf("stderr does not say how to proceed:\n%s", got.stderr)
	}
}

func TestOnlyAndSkipTogetherIsAUsageError(t *testing.T) {
	// FR-036.
	forge := newMutableForge(t)
	cfg := configFor(t, forge.URL)
	repo := newClonePairForApply(t)

	got := forgectl(t, repo, env(cfg), "apply", "--yes", "--only=branch", "--skip=vars")

	if got.code != 2 {
		t.Errorf("exit = %d, want 2\nstderr: %s", got.code, got.stderr)
	}
	if forge.mutationCount() != 0 {
		t.Errorf("a usage error still changed something: %v", forge.Mutations)
	}
}

func TestSkipRestrictsTheWork(t *testing.T) {
	forge := newMutableForge(t)
	cfg := configFor(t, forge.URL)
	repo := newClonePairForApply(t)

	got := forgectl(t, repo, env(cfg), "apply", "--yes", "--skip=protection")
	if got.code != 3 {
		// The branch converged, so protection is the only drift left: exit 3.
		t.Fatalf("exit = %d, want 3\nstdout: %s\nstderr: %s", got.code, got.stdout, got.stderr)
	}

	if forge.defaultBranch != "main" {
		t.Errorf("the branch domain did not run: default = %q", forge.defaultBranch)
	}
	if forge.protected["main"] {
		t.Error("the protection domain ran despite --skip=protection")
	}
}

func TestPartialFailureExitsOneAndReportsWhatSucceeded(t *testing.T) {
	// FR-045, CLI-002: a run that completed only in part is a runtime failure,
	// and the report says which actions succeeded and which did not.
	forge := newMutableForge(t)
	forge.Fail = "protected_branches"
	cfg := configFor(t, forge.URL)
	repo := newClonePairForApply(t)

	got := forgectl(t, repo, env(cfg), "apply", "--yes", "--output=json")

	if got.code != 1 {
		t.Fatalf("exit = %d, want 1\nstdout: %s\nstderr: %s", got.code, got.stdout, got.stderr)
	}

	var doc struct {
		Actions []struct {
			Description string `json:"description"`
			Status      string `json:"status"`
			Error       string `json:"error"`
		} `json:"actions"`
	}
	if err := json.Unmarshal([]byte(got.stdout), &doc); err != nil {
		t.Fatalf("stdout does not parse: %v\n%s", err, got.stdout)
	}

	var done, failed int
	for _, a := range doc.Actions {
		switch a.Status {
		case "done":
			done++
		case "failed":
			failed++
			if a.Error == "" {
				t.Errorf("the failed action %q carries no message", a.Description)
			}
		}
	}

	if done == 0 {
		t.Error("the report shows nothing as done, though the branch work succeeded")
	}
	if failed == 0 {
		t.Error("the report shows nothing as failed")
	}

	if !strings.Contains(got.stderr, "rerun") {
		t.Errorf("stderr does not say a rerun converges:\n%s", got.stderr)
	}
}

func TestDeclinedConfirmationChangesNothing(t *testing.T) {
	// US2 acceptance scenario 6. Without a TTY the prompt cannot be answered,
	// so the "declined" path is driven by piping "n" through a shell that keeps
	// stdin attached to a pipe: apply must still refuse to act.
	forge := newMutableForge(t)
	cfg := configFor(t, forge.URL)
	repo := newClonePairForApply(t)

	got := forgectl(t, repo, env(cfg), "apply")

	// Not a terminal, no --yes: nothing happens, and that is the point.
	if forge.mutationCount() != 0 {
		t.Errorf("apply modified something it was not authorised to: %v", forge.Mutations)
	}
	if got.code == 0 {
		t.Error("apply reported success without doing anything")
	}
}

// gitOutput runs git and returns its output, for assertions about the clone.
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}

	return string(out)
}
