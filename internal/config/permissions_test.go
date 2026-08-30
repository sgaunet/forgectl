package config_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sgaunet/forgectl/internal/config"
)

func TestWideConfigPermissionsAreRefused(t *testing.T) {
	path := writeConfig(t, "settings:\n  default_branch: main\n")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	_, err := config.Load(config.Options{Path: path, PathSet: true}, config.Environment{})
	if err == nil {
		t.Fatal("Load accepted a world-readable configuration file")
	}
	if !errors.Is(err, config.ErrPermissions) {
		t.Fatalf("error %v does not wrap ErrPermissions", err)
	}

	// FR-007: the refusal names the file, its mode, and the command that fixes it.
	msg := err.Error()
	for _, want := range []string{path, "0644", "chmod 0600"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not contain %q", msg, want)
		}
	}
}

func TestWidePermissionsAreBypassableWithTheFlag(t *testing.T) {
	path := writeConfig(t, "settings:\n  default_branch: main\n")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	if _, err := config.Load(config.Options{
		Path: path, PathSet: true,
		AllowInsecure: true, AllowInsecureSet: true,
	}, config.Environment{}); err != nil {
		t.Fatalf("--allow-insecure-config did not bypass the permission check: %v", err)
	}
}

func TestOwnerOnlyPermissionsAreAccepted(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o400, 0o200} {
		path := writeConfig(t, "settings:\n  default_branch: main\n")
		if err := os.Chmod(path, mode); err != nil {
			t.Fatalf("Chmod: %v", err)
		}
		if err := config.CheckPermissions(path, false); err != nil {
			t.Errorf("mode %04o was refused: %v", mode, err)
		}
	}
}

// newWorkingCopy builds a git working copy for the in-repository refusal tests.
func newWorkingCopy(t *testing.T) string {
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

func TestValuesFileInsideTheRepositoryIsRefused(t *testing.T) {
	repo := newWorkingCopy(t)
	varFile := filepath.Join(repo, "vars.yaml")
	if err := os.WriteFile(varFile, []byte("values:\n  k: v\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := config.CheckNotInRepository(context.Background(), repo, varFile)
	if err == nil {
		t.Fatal("a values file that git does not ignore was accepted")
	}
	if !errors.Is(err, config.ErrValuesInRepo) {
		t.Fatalf("error %v does not wrap ErrValuesInRepo", err)
	}

	// FR-056: the refusal names the file and the ignore entry that resolves it.
	msg := err.Error()
	if !strings.Contains(msg, "vars.yaml") || !strings.Contains(msg, ".gitignore") {
		t.Errorf("message %q names neither the file nor the ignore entry", msg)
	}
	// FR-056: and it says the refusal cannot be bypassed.
	if !strings.Contains(msg, "no bypass") {
		t.Errorf("message %q does not state that the refusal has no bypass", msg)
	}
}

func TestValuesFileIgnoredByGitIsAccepted(t *testing.T) {
	repo := newWorkingCopy(t)
	varFile := filepath.Join(repo, "vars.yaml")

	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("vars.yaml\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(varFile, []byte("values:\n  k: v\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// The same file, ignored, is accepted without a warning: a value that
	// cannot be committed is not in the repository.
	if err := config.CheckNotInRepository(context.Background(), repo, varFile); err != nil {
		t.Fatalf("an ignored values file was refused: %v", err)
	}
}

func TestValuesFileOutsideTheRepositoryIsAccepted(t *testing.T) {
	repo := newWorkingCopy(t)
	outside := filepath.Join(t.TempDir(), "vars.yaml")
	if err := os.WriteFile(outside, []byte("values:\n  k: v\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := config.CheckNotInRepository(context.Background(), repo, outside); err != nil {
		t.Fatalf("a values file outside the working copy was refused: %v", err)
	}
}

func TestInRepositoryRefusalPrecedesAnyRead(t *testing.T) {
	// FR-057: the refusal is raised before any value is read from the file, so
	// a file that does not even exist yet is still refused for its location.
	repo := newWorkingCopy(t)
	absent := filepath.Join(repo, "not-written-yet.yaml")

	err := config.CheckNotInRepository(context.Background(), repo, absent)
	if !errors.Is(err, config.ErrValuesInRepo) {
		t.Fatalf("error = %v, want ErrValuesInRepo raised without reading the file", err)
	}
}
