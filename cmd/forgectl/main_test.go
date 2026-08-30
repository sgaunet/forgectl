package main_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// binary builds cmd/forgectl once per test run and returns its path. The
// end-to-end tests drive the real binary, so the exit codes and the stream
// split are exercised exactly as a caller sees them (Constitution VII).
var binary = sync.OnceValues(func() (string, error) {
	dir, err := os.MkdirTemp("", "forgectl-e2e")
	if err != nil {
		return "", err
	}

	path := filepath.Join(dir, "forgectl")
	cmd := exec.CommandContext(context.Background(), "go", "build", "-o", path, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", &buildError{output: string(out), err: err}
	}

	return path, nil
})

// buildError reports a failure to build the binary under test.
type buildError struct {
	output string
	err    error
}

func (e *buildError) Error() string { return e.err.Error() + "\n" + e.output }
func (e *buildError) Unwrap() error { return e.err }

// result is one invocation of the binary.
type result struct {
	stdout string
	stderr string
	code   int
}

// forgectl runs the binary in dir with the given environment additions.
func forgectl(t *testing.T, dir string, env []string, args ...string) result {
	t.Helper()

	path, err := binary()
	if err != nil {
		t.Fatalf("building forgectl: %v", err)
	}

	cmd := exec.CommandContext(t.Context(), path, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	_ = cmd.Run()

	return result{stdout: stdout.String(), stderr: stderr.String(), code: cmd.ProcessState.ExitCode()}
}

// git runs a git command, failing the test if it does not succeed.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// newRepo builds a working copy on the named branch with a remote pointing at
// the given host.
func newRepo(t *testing.T, branch, remoteURL string) string {
	t.Helper()

	dir := t.TempDir()
	git(t, dir, "init", "-b", branch)
	git(t, dir, "commit", "--allow-empty", "-m", "init")
	git(t, dir, "remote", "add", "origin", remoteURL)

	return dir
}

// emptyConfig writes an owner-only configuration file that configures nothing,
// so a test never reads the developer's own ~/.config/forgectl/config.yaml.
func emptyConfig(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("settings:\n  default_branch: main\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	return path
}

func TestDetectReportsTheDetectionFacts(t *testing.T) {
	repo := newRepo(t, "master", "git@gitlab.com:acme/my-tool.git")
	cfg := emptyConfig(t)

	got := forgectl(t, repo, []string{"GITLAB_TOKEN=glpat-test", "FORGECTL_CONFIG=" + cfg}, "detect")

	if got.code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr: %s", got.code, got.stderr)
	}

	// FR-004: owner and name, instance, host, platform, and API base URL.
	for _, want := range []string{
		"acme/my-tool", "gitlab.com", "gitlab", "https://gitlab.com/api/v4",
	} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("stdout omits %q:\n%s", want, got.stdout)
		}
	}
}

func TestDetectWorksFromASubdirectory(t *testing.T) {
	// US1 acceptance scenario 4: the enclosing working copy is found and used.
	repo := newRepo(t, "main", "https://github.com/acme/my-tool.git")
	cfg := emptyConfig(t)

	sub := filepath.Join(repo, "internal", "deep")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	got := forgectl(t, sub, []string{"GITHUB_TOKEN=ghp-test", "FORGECTL_CONFIG=" + cfg}, "detect")

	if got.code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr: %s", got.code, got.stderr)
	}
	if !strings.Contains(got.stdout, "acme/my-tool") {
		t.Errorf("stdout does not name the repository:\n%s", got.stdout)
	}
}

func TestJSONOutputIsACleanDocumentOnStdout(t *testing.T) {
	// CLI-001: stdout carries data only, so it parses even when stderr is busy.
	repo := newRepo(t, "master", "git@gitlab.com:acme/my-tool.git")
	cfg := emptyConfig(t)

	got := forgectl(t, repo,
		[]string{"GITLAB_TOKEN=glpat-test", "FORGECTL_CONFIG=" + cfg},
		"detect", "--output=json")

	if got.code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr: %s", got.code, got.stderr)
	}

	var doc map[string]any
	if err := json.Unmarshal([]byte(got.stdout), &doc); err != nil {
		t.Fatalf("stdout does not parse as JSON: %v\n%s", err, got.stdout)
	}
	if doc["command"] != "detect" {
		t.Errorf("command = %v, want detect", doc["command"])
	}
	if doc["repository"] != "acme/my-tool" {
		t.Errorf("repository = %v, want acme/my-tool", doc["repository"])
	}
}

func TestJSONAliasMatchesTheOutputFlag(t *testing.T) {
	repo := newRepo(t, "main", "git@gitlab.com:acme/my-tool.git")
	cfg := emptyConfig(t)
	env := []string{"GITLAB_TOKEN=glpat-test", "FORGECTL_CONFIG=" + cfg}

	viaAlias := forgectl(t, repo, env, "detect", "--json")
	viaFlag := forgectl(t, repo, env, "detect", "--output=json")

	if viaAlias.stdout != viaFlag.stdout {
		t.Errorf("--json and --output=json differ:\n%s\n---\n%s", viaAlias.stdout, viaFlag.stdout)
	}
}

func TestNoColourWhenStdoutIsNotATerminal(t *testing.T) {
	// Constitution V: no colour, spinners, or progress when stdout is not a TTY.
	// The test captures stdout through a pipe, which is exactly that case.
	repo := newRepo(t, "main", "git@gitlab.com:acme/my-tool.git")
	cfg := emptyConfig(t)

	got := forgectl(t, repo, []string{"GITLAB_TOKEN=glpat-test", "FORGECTL_CONFIG=" + cfg}, "detect")

	if strings.Contains(got.stdout, "\x1b[") {
		t.Errorf("stdout carries ANSI escapes despite not being a terminal:\n%q", got.stdout)
	}
}

func TestUsageErrorsExitTwo(t *testing.T) {
	// CLI-002: exit 2 covers every condition where nothing was attempted.
	repo := newRepo(t, "main", "git@gitlab.com:acme/my-tool.git")
	cfg := emptyConfig(t)
	withToken := []string{"GITLAB_TOKEN=glpat-test", "FORGECTL_CONFIG=" + cfg}

	tests := []struct {
		name string
		dir  string
		env  []string
		args []string
	}{
		{name: "an unknown flag", dir: repo, env: withToken, args: []string{"detect", "--bogus"}},
		{
			name: "--verbose with --quiet",
			dir:  repo, env: withToken,
			args: []string{"detect", "--verbose", "--quiet"},
		},
		{
			name: "an unknown output format",
			dir:  repo, env: withToken,
			args: []string{"detect", "--output=yaml"},
		},
		{
			name: "outside a working copy",
			dir:  t.TempDir(), env: withToken,
			args: []string{"detect"},
		},
		{
			name: "an unknown remote",
			dir:  repo, env: withToken,
			args: []string{"detect", "--remote=upstream"},
		},
		{
			name: "an unset credential",
			dir:  repo, env: []string{"GITLAB_TOKEN=", "FORGECTL_CONFIG=" + cfg},
			args: []string{"detect"},
		},
		{
			name: "an unknown profile",
			dir:  repo, env: withToken,
			args: []string{"check", "no-such-profile"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := forgectl(t, tt.dir, tt.env, tt.args...)

			if got.code != 2 {
				t.Errorf("exit = %d, want 2\nstdout: %s\nstderr: %s", got.code, got.stdout, got.stderr)
			}
			// The message belongs on stderr; stdout stays clean.
			if strings.TrimSpace(got.stdout) != "" {
				t.Errorf("stdout is not empty on a usage error:\n%s", got.stdout)
			}
			if strings.TrimSpace(got.stderr) == "" {
				t.Error("stderr carries no explanation of the usage error")
			}
		})
	}
}

func TestUnknownHostExitsTwoNamingTheConfigFile(t *testing.T) {
	repo := newRepo(t, "main", "git@git.example.com:acme/my-tool.git")
	cfg := emptyConfig(t)

	got := forgectl(t, repo, []string{"FORGECTL_CONFIG=" + cfg}, "detect")

	if got.code != 2 {
		t.Fatalf("exit = %d, want 2\nstderr: %s", got.code, got.stderr)
	}
	// FR-003: the message names the unknown host and where to declare it.
	if !strings.Contains(got.stderr, "git.example.com") || !strings.Contains(got.stderr, cfg) {
		t.Errorf("stderr names neither the host nor the config file:\n%s", got.stderr)
	}
}

func TestWideConfigPermissionsExitTwo(t *testing.T) {
	repo := newRepo(t, "main", "git@gitlab.com:acme/my-tool.git")
	cfg := emptyConfig(t)
	if err := os.Chmod(cfg, 0o644); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	got := forgectl(t, repo, []string{"GITLAB_TOKEN=glpat-test", "FORGECTL_CONFIG=" + cfg}, "detect")

	if got.code != 2 {
		t.Fatalf("exit = %d, want 2\nstderr: %s", got.code, got.stderr)
	}
	if !strings.Contains(got.stderr, "chmod 0600") {
		t.Errorf("stderr does not name the command that fixes it:\n%s", got.stderr)
	}

	// FR-007: and the bypass flag lets it through.
	got = forgectl(t, repo,
		[]string{"GITLAB_TOKEN=glpat-test", "FORGECTL_CONFIG=" + cfg},
		"detect", "--allow-insecure-config")
	if got.code != 0 {
		t.Errorf("--allow-insecure-config did not bypass the check: exit %d\n%s", got.code, got.stderr)
	}
}

func TestHelpDocumentsTheContract(t *testing.T) {
	// CLI-002 and CLI-004 both require the contract to be stated in --help.
	got := forgectl(t, t.TempDir(), nil, "--help")

	if got.code != 0 {
		t.Fatalf("exit = %d, want 0", got.code)
	}

	for _, want := range []string{
		"Exit codes", "0", "1", "2", "3",
		"flags > environment > config file > defaults",
		"git must be on PATH",
	} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("--help does not state %q", want)
		}
	}
}
