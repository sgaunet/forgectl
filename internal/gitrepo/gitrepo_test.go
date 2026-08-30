package gitrepo_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sgaunet/forgectl/internal/gitrepo"
)

// git runs a git command inside dir and fails the test if it does not succeed.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=forgectl test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=forgectl test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}

	return string(out)
}

// newRepo builds a working copy with one commit on the named branch and an
// origin remote pointing at the given URL.
func newRepo(t *testing.T, branch, remoteURL string) string {
	t.Helper()

	dir := t.TempDir()
	git(t, dir, "init", "-b", branch)
	git(t, dir, "commit", "--allow-empty", "-m", "init")

	if remoteURL != "" {
		git(t, dir, "remote", "add", "origin", remoteURL)
	}

	return dir
}

func TestDiscoverFromRepositoryRoot(t *testing.T) {
	dir := newRepo(t, "master", "git@gitlab.example.com:acme/my-tool.git")

	wc, err := gitrepo.Discover(context.Background(), dir, "origin")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if wc.Remote != "origin" {
		t.Errorf("Remote = %q, want origin", wc.Remote)
	}
	if wc.RemoteURL != "git@gitlab.example.com:acme/my-tool.git" {
		t.Errorf("RemoteURL = %q", wc.RemoteURL)
	}

	// The root git reports may be a symlink-resolved form of the temporary
	// directory, so compare the resolved paths.
	wantRoot, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	gotRoot, err := filepath.EvalSymlinks(wc.Root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if gotRoot != wantRoot {
		t.Errorf("Root = %q, want %q", gotRoot, wantRoot)
	}
}

func TestDiscoverFromSubdirectory(t *testing.T) {
	dir := newRepo(t, "main", "https://github.com/sgaunet/forgectl.git")

	sub := filepath.Join(dir, "internal", "deep")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	wc, err := gitrepo.Discover(context.Background(), sub, "origin")
	if err != nil {
		t.Fatalf("Discover from subdirectory: %v", err)
	}

	ref, err := wc.Ref()
	if err != nil {
		t.Fatalf("Ref: %v", err)
	}
	if ref.String() != "sgaunet/forgectl" {
		t.Errorf("Ref = %q, want sgaunet/forgectl", ref.String())
	}
}

func TestDiscoverOutsideAWorkingCopy(t *testing.T) {
	// t.TempDir() is not a git working copy, and neither is any parent of it
	// on a machine whose temporary directory is not tracked.
	dir := t.TempDir()

	_, err := gitrepo.Discover(context.Background(), dir, "origin")
	if err == nil {
		t.Fatal("Discover succeeded outside a working copy, want ErrNotARepo")
	}
	if !errors.Is(err, gitrepo.ErrNotARepo) {
		t.Errorf("error %v does not wrap ErrNotARepo", err)
	}
}

func TestDiscoverWithNoCommits(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-b", "main")
	git(t, dir, "remote", "add", "origin", "https://github.com/sgaunet/forgectl.git")

	_, err := gitrepo.Discover(context.Background(), dir, "origin")
	if err == nil {
		t.Fatal("Discover succeeded on a repository with no commits, want ErrNoCommits")
	}
	if !errors.Is(err, gitrepo.ErrNoCommits) {
		t.Errorf("error %v does not wrap ErrNoCommits", err)
	}
}

func TestDiscoverWithUnknownRemote(t *testing.T) {
	dir := newRepo(t, "main", "https://github.com/sgaunet/forgectl.git")

	_, err := gitrepo.Discover(context.Background(), dir, "upstream")
	if err == nil {
		t.Fatal("Discover succeeded with an unknown remote, want ErrNoRemote")
	}
	if !errors.Is(err, gitrepo.ErrNoRemote) {
		t.Errorf("error %v does not wrap ErrNoRemote", err)
	}
	// The message must name the remote that was asked for and list those that
	// do exist, so the maintainer can correct the invocation.
	if msg := err.Error(); !strings.Contains(msg, "upstream") || !strings.Contains(msg, "origin") {
		t.Errorf("error %q names neither the missing remote nor the existing ones", msg)
	}
}

func TestDiscoverWithDetachedHEAD(t *testing.T) {
	dir := newRepo(t, "main", "https://github.com/sgaunet/forgectl.git")
	git(t, dir, "checkout", "--detach", "HEAD")

	// Every operation targets a named ref, so a detached HEAD is normal.
	if _, err := gitrepo.Discover(context.Background(), dir, "origin"); err != nil {
		t.Fatalf("Discover with a detached HEAD: %v", err)
	}
}

func TestDiscoverWithoutGitOnPATH(t *testing.T) {
	dir := newRepo(t, "main", "https://github.com/sgaunet/forgectl.git")
	t.Setenv("PATH", "")

	_, err := gitrepo.Discover(context.Background(), dir, "origin")
	if err == nil {
		t.Fatal("Discover succeeded with no git on PATH, want ErrGitMissing")
	}
	if !errors.Is(err, gitrepo.ErrGitMissing) {
		t.Errorf("error %v does not wrap ErrGitMissing", err)
	}
}

func TestRefRejectsAnUnparseableRemoteURL(t *testing.T) {
	dir := newRepo(t, "main", "/srv/git/local.git")

	wc, err := gitrepo.Discover(context.Background(), dir, "origin")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if _, err := wc.Ref(); !errors.Is(err, gitrepo.ErrRemoteURL) {
		t.Errorf("Ref error = %v, want ErrRemoteURL", err)
	}
}
