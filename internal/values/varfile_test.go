package values_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sgaunet/forgectl/internal/config"
	"github.com/sgaunet/forgectl/internal/values"
)

// newRepo builds a git working copy for the in-repository refusal tests.
func newRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
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

	return dir
}

// writeVarFile writes an override file at mode 0600.
func writeVarFile(t *testing.T, dir, name, body string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	return path
}

func TestVarFileInsideTheRepositoryIsRefused(t *testing.T) {
	// FR-056: a values file that git does not ignore is one `git add` away
	// from being published.
	repo := newRepo(t)
	path := writeVarFile(t, repo, "vars.yaml", "values:\n  token: a-value\n")

	_, err := values.LoadVarFile(context.Background(), path, repo)
	if err == nil {
		t.Fatal("a values file that git does not ignore was accepted")
	}
	if !errors.Is(err, config.ErrValuesInRepo) {
		t.Fatalf("error %v does not wrap ErrValuesInRepo", err)
	}

	msg := err.Error()
	if !strings.Contains(msg, "vars.yaml") || !strings.Contains(msg, ".gitignore") {
		t.Errorf("message %q names neither the file nor the ignore entry", msg)
	}
}

func TestVarFileIgnoredByGitIsAccepted(t *testing.T) {
	// The same file, ignored, is accepted with no warning: a value that cannot
	// be committed is not in the repository.
	repo := newRepo(t)
	writeVarFile(t, repo, ".gitignore", "vars.yaml\n")
	path := writeVarFile(t, repo, "vars.yaml", "values:\n  token: a-value\n")

	file, err := values.LoadVarFile(context.Background(), path, repo)
	if err != nil {
		t.Fatalf("an ignored values file was refused: %v", err)
	}
	if file.Values["token"] != "a-value" {
		t.Errorf("the file was not parsed: %v", file.Values)
	}
}

func TestVarFileOutsideTheRepositoryIsAccepted(t *testing.T) {
	repo := newRepo(t)
	path := writeVarFile(t, t.TempDir(), "vars.yaml", "values:\n  token: a-value\n")

	if _, err := values.LoadVarFile(context.Background(), path, repo); err != nil {
		t.Errorf("a values file outside the working copy was refused: %v", err)
	}
}

func TestTheRefusalPrecedesAnyRead(t *testing.T) {
	// FR-057: the refusal is raised before any value is read from the file. A
	// file that does not exist yet is still refused for its location, which is
	// the clearest proof that nothing was read first.
	repo := newRepo(t)
	path := filepath.Join(repo, "not-written-yet.yaml")

	_, err := values.LoadVarFile(context.Background(), path, repo)
	if !errors.Is(err, config.ErrValuesInRepo) {
		t.Fatalf("error = %v, want ErrValuesInRepo raised without reading the file", err)
	}
}

func TestVarFilePermissionsAreEnforced(t *testing.T) {
	// The override file holds values, so its permissions matter exactly as the
	// configuration file's do (FR-007).
	path := writeVarFile(t, t.TempDir(), "vars.yaml", "values:\n  token: a-value\n")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	_, err := values.LoadVarFile(context.Background(), path, "")
	if !errors.Is(err, config.ErrPermissions) {
		t.Errorf("error = %v, want ErrPermissions", err)
	}
}

func TestNoVarFileIsNotAnError(t *testing.T) {
	file, err := values.LoadVarFile(context.Background(), "", "")
	if err != nil {
		t.Fatalf("LoadVarFile with no path: %v", err)
	}
	if len(file.Values) != 0 {
		t.Errorf("values = %v, want empty", file.Values)
	}
}
