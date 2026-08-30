package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// IsIgnored reports whether git ignores path, which must lie inside the working
// copy rooted at root. A path outside the working copy is not ignored — git
// cannot reason about it — and is reported as such rather than as an error.
//
// This backs the refusal of FR-056: a file holding values that git does NOT
// ignore is one `git add` away from being published, so forgectl refuses to run.
func IsIgnored(ctx context.Context, root, path string) (bool, error) {
	inside, err := IsInside(root, path)
	if err != nil {
		return false, err
	}
	if !inside {
		return false, nil
	}

	// check-ignore exits 0 when the path is ignored, 1 when it is not, and
	// anything else on a real failure.
	cmd := exec.CommandContext(ctx, "git", "check-ignore", "-q", "--", path)
	cmd.Dir = root

	err = cmd.Run()
	if err == nil {
		return true, nil
	}

	if errors.Is(err, exec.ErrNotFound) {
		return false, fmt.Errorf("%w: forgectl runs git for local work", ErrGitMissing)
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}

	return false, fmt.Errorf("git check-ignore %s: %w", path, err)
}

// IsInside reports whether path lies within the working copy rooted at root.
// Both are resolved through symlinks first, so a temporary directory reached by
// two different names still compares equal.
func IsInside(root, path string) (bool, error) {
	resolvedRoot, err := resolve(root)
	if err != nil {
		return false, err
	}

	resolvedPath, err := resolve(path)
	if err != nil {
		return false, err
	}

	rel, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil {
		return false, nil //nolint:nilerr // an unrelatable path is simply outside
	}

	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
}

// resolve makes a path absolute and follows symlinks as far as the filesystem
// allows. A file that does not exist yet still resolves through its parent,
// which is what lets FR-057 raise the refusal before the file is read.
func resolve(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", path, err)
	}

	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}

	// The leaf may not exist. Resolve the directory holding it instead.
	dir, base := filepath.Split(abs)
	resolvedDir, err := filepath.EvalSymlinks(filepath.Clean(dir))
	if err != nil {
		return abs, nil //nolint:nilerr // nothing resolvable: the absolute path stands
	}

	return filepath.Join(resolvedDir, base), nil
}
