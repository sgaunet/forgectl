package gitrepo_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sgaunet/forgectl/internal/gitrepo"
)

// newClonePair builds a bare repository standing in for the remote, and a
// working copy cloned from it on the named branch. Push, set-head, and delete
// are therefore exercised against a real remote rather than a stub (R12).
func newClonePair(t *testing.T) *gitrepo.WorkingCopy {
	t.Helper()

	// Every branch test starts from the state the convention moves away from.
	const branch = "master"

	root := t.TempDir()
	bare := filepath.Join(root, "remote.git")
	work := filepath.Join(root, "work")

	git(t, root, "init", "--bare", "-b", branch, bare)
	git(t, root, "clone", bare, work)
	git(t, work, "commit", "--allow-empty", "-m", "init")
	git(t, work, "push", "-u", "origin", branch)

	wc, err := gitrepo.Discover(context.Background(), work, "origin")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	return wc
}

// branches lists the local branches of a working copy.
func branches(t *testing.T, wc *gitrepo.WorkingCopy) string {
	t.Helper()

	return git(t, wc.Root, "branch", "--format=%(refname:short)")
}

// remoteBranches lists the branches the remote actually holds.
func remoteBranches(t *testing.T, wc *gitrepo.WorkingCopy) string {
	t.Helper()

	return git(t, wc.Root, "ls-remote", "--heads", "origin")
}

func TestRenameAndPushBranch(t *testing.T) {
	// FR-037: the master-with-no-main path — rename, push with tracking, then
	// set the remote head.
	wc := newClonePair(t)
	ctx := context.Background()

	if err := wc.RenameBranch(ctx, "master", "main"); err != nil {
		t.Fatalf("RenameBranch: %v", err)
	}
	if got := branches(t, wc); !strings.Contains(got, "main") || strings.Contains(got, "master") {
		t.Errorf("local branches = %q, want master renamed to main", got)
	}

	if err := wc.PushWithUpstream(ctx, "main"); err != nil {
		t.Fatalf("PushWithUpstream: %v", err)
	}
	if got := remoteBranches(t, wc); !strings.Contains(got, "refs/heads/main") {
		t.Errorf("remote branches = %q, want main pushed", got)
	}

	// The push set upstream tracking, so the branch has somewhere to go next time.
	upstream := git(t, wc.Root, "rev-parse", "--abbrev-ref", "main@{upstream}")
	if strings.TrimSpace(upstream) != "origin/main" {
		t.Errorf("upstream = %q, want origin/main", strings.TrimSpace(upstream))
	}
}

func TestCreateBranchFromRemote(t *testing.T) {
	// FR-037: when the new name is absent locally, it is created from the
	// remote branch rather than from whatever HEAD happens to be.
	wc := newClonePair(t)
	ctx := context.Background()

	if err := wc.CreateBranchFromRemote(ctx, "main", "master"); err != nil {
		t.Fatalf("CreateBranchFromRemote: %v", err)
	}

	if got := branches(t, wc); !strings.Contains(got, "main") {
		t.Errorf("local branches = %q, want main created", got)
	}

	// It points at the same commit as the remote branch it came from.
	fromRemote := strings.TrimSpace(git(t, wc.Root, "rev-parse", "origin/master"))
	created := strings.TrimSpace(git(t, wc.Root, "rev-parse", "main"))
	if created != fromRemote {
		t.Errorf("main is at %s, want %s (origin/master)", created, fromRemote)
	}
}

func TestSetRemoteHead(t *testing.T) {
	wc := newClonePair(t)
	ctx := context.Background()

	if err := wc.RenameBranch(ctx, "master", "main"); err != nil {
		t.Fatalf("RenameBranch: %v", err)
	}
	if err := wc.PushWithUpstream(ctx, "main"); err != nil {
		t.Fatalf("PushWithUpstream: %v", err)
	}
	if err := wc.SetRemoteHead(ctx, "main"); err != nil {
		t.Fatalf("SetRemoteHead: %v", err)
	}

	head := strings.TrimSpace(git(t, wc.Root, "symbolic-ref", "refs/remotes/origin/HEAD"))
	if head != "refs/remotes/origin/main" {
		t.Errorf("remote head = %q, want refs/remotes/origin/main", head)
	}
}

func TestDeleteRemoteBranch(t *testing.T) {
	// FR-041: the old remote branch goes only when explicitly asked for, and
	// only once the new default is in place.
	wc := newClonePair(t)
	ctx := context.Background()

	if err := wc.CreateBranchFromRemote(ctx, "main", "master"); err != nil {
		t.Fatalf("CreateBranchFromRemote: %v", err)
	}
	if err := wc.PushWithUpstream(ctx, "main"); err != nil {
		t.Fatalf("PushWithUpstream: %v", err)
	}

	// The bare repository's HEAD still points at master, so it must be moved
	// before master can be deleted — exactly the ordering FR-041 requires.
	git(t, wc.Root+"/../remote.git", "symbolic-ref", "HEAD", "refs/heads/main")

	if err := wc.DeleteRemoteBranch(ctx, "master"); err != nil {
		t.Fatalf("DeleteRemoteBranch: %v", err)
	}

	if got := remoteBranches(t, wc); strings.Contains(got, "refs/heads/master") {
		t.Errorf("remote branches = %q, want master deleted", got)
	}
	if got := remoteBranches(t, wc); !strings.Contains(got, "refs/heads/main") {
		t.Errorf("remote branches = %q, want main to survive", got)
	}
}

func TestLocalAndRemoteBranchExistence(t *testing.T) {
	wc := newClonePair(t)
	ctx := context.Background()

	tests := []struct {
		name       string
		branch     string
		wantLocal  bool
		wantRemote bool
	}{
		{name: "the checked-out branch", branch: "master", wantLocal: true, wantRemote: true},
		{name: "a branch that does not exist", branch: "main", wantLocal: false, wantRemote: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			local, err := wc.LocalBranchExists(ctx, tt.branch)
			if err != nil {
				t.Fatalf("LocalBranchExists: %v", err)
			}
			if local != tt.wantLocal {
				t.Errorf("LocalBranchExists(%s) = %v, want %v", tt.branch, local, tt.wantLocal)
			}

			remote, err := wc.RemoteBranchExists(ctx, tt.branch)
			if err != nil {
				t.Fatalf("RemoteBranchExists: %v", err)
			}
			if remote != tt.wantRemote {
				t.Errorf("RemoteBranchExists(%s) = %v, want %v", tt.branch, remote, tt.wantRemote)
			}
		})
	}
}

func TestRetargetHintNamesBothBranchesAndTheRemote(t *testing.T) {
	// FR-040: the maintainer is given the command other clones must run.
	wc := newClonePair(t)

	hint := wc.RetargetHint("master", "main")
	for _, want := range []string{"master", "main", "origin", "git branch -m", "git fetch"} {
		if !strings.Contains(hint, want) {
			t.Errorf("hint %q does not contain %q", hint, want)
		}
	}
}
