package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// The sentinel errors every caller classifies on. All four mean the invocation
// was wrong and nothing was attempted, so cmd/forgectl maps them to exit 2
// (R11, CLI-002).
var (
	// ErrNotARepo reports a working directory outside any git working copy.
	ErrNotARepo = errors.New("not inside a git working copy")
	// ErrNoCommits reports a repository that has no commit yet, so it has no
	// branch to compare or converge.
	ErrNoCommits = errors.New("the repository has no commits")
	// ErrNoRemote reports a remote the working copy does not declare.
	ErrNoRemote = errors.New("no such remote")
	// ErrGitMissing reports that the git binary is not on PATH. It is the one
	// runtime dependency the static binary does not eliminate (R6).
	ErrGitMissing = errors.New("git is not on PATH")
)

// WorkingCopy is the local clone forgectl was pointed at.
type WorkingCopy struct {
	// Root is the top level of the working copy, found from any subdirectory.
	Root string
	// Remote is the name of the remote that was inspected, default "origin".
	Remote string
	// RemoteURL is that remote's URL, raw, exactly as git reports it.
	RemoteURL string
}

// Discover finds the working copy enclosing dir and reads the URL of the named
// remote (FR-001, FR-002). It fails with a distinct error for each of the four
// conditions a maintainer can actually correct.
func Discover(ctx context.Context, dir, remote string) (*WorkingCopy, error) {
	root, err := run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		if errors.Is(err, ErrGitMissing) {
			return nil, err
		}

		return nil, fmt.Errorf("%w: %s", ErrNotARepo, dir)
	}

	if _, err := run(ctx, dir, "rev-parse", "--verify", "HEAD"); err != nil {
		if errors.Is(err, ErrGitMissing) {
			return nil, err
		}

		return nil, fmt.Errorf("%w: %s", ErrNoCommits, root)
	}

	url, err := run(ctx, dir, "remote", "get-url", remote)
	if err != nil {
		if errors.Is(err, ErrGitMissing) {
			return nil, err
		}

		return nil, unknownRemote(ctx, dir, remote)
	}

	return &WorkingCopy{Root: root, Remote: remote, RemoteURL: url}, nil
}

// Ref derives the repository this working copy points at from its remote URL.
func (w *WorkingCopy) Ref() (RemoteRef, error) {
	ref, err := ParseRemoteURL(w.RemoteURL)
	if err != nil {
		return RemoteRef{}, fmt.Errorf("remote %q: %w", w.Remote, err)
	}

	return ref, nil
}

// unknownRemote builds the error for a remote that does not exist, listing the
// ones that do so the maintainer can correct the invocation without a second
// command.
func unknownRemote(ctx context.Context, dir, remote string) error {
	known, err := run(ctx, dir, "remote")
	if err != nil || known == "" {
		return fmt.Errorf("%w: %q; this working copy declares no remote", ErrNoRemote, remote)
	}

	names := strings.Join(strings.Fields(known), ", ")

	return fmt.Errorf("%w: %q; this working copy declares: %s", ErrNoRemote, remote, names)
}

// run executes one git command in dir and returns its trimmed standard output.
// Every git call in forgectl goes through here, so cancellation, the missing
// binary, and stderr capture are handled in exactly one place.
func run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("%w: forgectl runs git for local work", ErrGitMissing)
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", fmt.Errorf("git %s: %w", args[0], ctxErr)
		}

		return "", fmt.Errorf("git %s: %w: %s",
			strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}

	return strings.TrimSpace(stdout.String()), nil
}

// LocalBranchExists reports whether the named branch exists in the working copy.
func (w *WorkingCopy) LocalBranchExists(ctx context.Context, name string) (bool, error) {
	_, err := run(ctx, w.Root, "rev-parse", "--verify", "--quiet", "refs/heads/"+name)
	if err != nil {
		if errors.Is(err, ErrGitMissing) {
			return false, err
		}

		return false, nil
	}

	return true, nil
}

// RemoteBranchExists reports whether the remote tracking branch exists locally.
func (w *WorkingCopy) RemoteBranchExists(ctx context.Context, name string) (bool, error) {
	ref := "refs/remotes/" + w.Remote + "/" + name

	_, err := run(ctx, w.Root, "rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		if errors.Is(err, ErrGitMissing) {
			return false, err
		}

		return false, nil
	}

	return true, nil
}

// RenameBranch renames a local branch (FR-037).
func (w *WorkingCopy) RenameBranch(ctx context.Context, from, to string) error {
	if _, err := run(ctx, w.Root, "branch", "-m", from, to); err != nil {
		return fmt.Errorf("renaming branch %s to %s: %w", from, to, err)
	}

	return nil
}

// CreateBranchFromRemote creates a local branch from the remote tracking branch,
// which is what apply does when the new name is absent locally (FR-037).
func (w *WorkingCopy) CreateBranchFromRemote(ctx context.Context, name, from string) error {
	start := w.Remote + "/" + from

	if _, err := run(ctx, w.Root, "branch", name, start); err != nil {
		return fmt.Errorf("creating branch %s from %s: %w", name, start, err)
	}

	return nil
}

// PushWithUpstream pushes a branch and sets it to track the remote (FR-037).
func (w *WorkingCopy) PushWithUpstream(ctx context.Context, name string) error {
	if _, err := run(ctx, w.Root, "push", "-u", w.Remote, name); err != nil {
		return fmt.Errorf("pushing branch %s to %s: %w", name, w.Remote, err)
	}

	return nil
}

// SetRemoteHead points the local remote head at the new default branch, so
// clones and `git remote show` agree with the platform (FR-037).
func (w *WorkingCopy) SetRemoteHead(ctx context.Context, name string) error {
	if _, err := run(ctx, w.Root, "remote", "set-head", w.Remote, name); err != nil {
		return fmt.Errorf("setting the remote head of %s to %s: %w", w.Remote, name, err)
	}

	return nil
}

// DeleteRemoteBranch deletes a branch on the remote.
//
// It is called only when the maintainer explicitly asked for it, and only after
// the new default branch is in place (FR-041): deleting the old branch while it
// is still the platform default would break every open merge request at once.
func (w *WorkingCopy) DeleteRemoteBranch(ctx context.Context, name string) error {
	if _, err := run(ctx, w.Root, "push", w.Remote, "--delete", name); err != nil {
		return fmt.Errorf("deleting branch %s on %s: %w", name, w.Remote, err)
	}

	return nil
}

// RetargetHint is the command every other clone must run after the default
// branch has been switched, printed as part of the warning FR-040 requires.
func (w *WorkingCopy) RetargetHint(oldName, newName string) string {
	return fmt.Sprintf(
		"git branch -m %s %s && git fetch %s && "+
			"git branch -u %s/%s %s && git remote set-head %s -a",
		oldName, newName, w.Remote, w.Remote, newName, newName, w.Remote)
}
