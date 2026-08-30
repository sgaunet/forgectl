package config

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/sgaunet/forgectl/internal/gitrepo"
)

var (
	// ErrPermissions reports a configuration or override file readable or
	// writable beyond its owner (FR-007). It is bypassable with
	// --allow-insecure-config, and with nothing else.
	ErrPermissions = errors.New("configuration file permissions are too wide")

	// ErrValuesInRepo reports a values-bearing file that lies inside the
	// working copy and that git does not ignore (FR-056). This refusal has NO
	// bypass: unlike ErrPermissions, the file is one `git add` away from being
	// published, so --allow-insecure-config must not override it.
	ErrValuesInRepo = errors.New("a file holding values is not ignored by git")
)

// ownerOnly is the widest mode a file holding values may carry.
const ownerOnly fs.FileMode = 0o600

// CheckPermissions refuses a file whose mode grants any access beyond its
// owner, naming the file, its current mode, and the command that corrects it
// (FR-007). A file that does not exist yields os.ErrNotExist, which the caller
// treats as "no configuration", not as a refusal.
func CheckPermissions(path string, allowInsecure bool) error {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return err //nolint:wrapcheck // the caller matches on os.ErrNotExist
		}

		return fmt.Errorf("inspecting %s: %w", path, err)
	}

	mode := info.Mode().Perm()
	if mode&^ownerOnly == 0 {
		return nil
	}

	if allowInsecure {
		return nil
	}

	return fmt.Errorf(
		"%w: %s is mode %04o; run: chmod 0600 %s",
		ErrPermissions, path, uint32(mode), path)
}

// CheckNotInRepository refuses a values-bearing file that lies inside the
// working copy while git does not ignore it, naming the file and the ignore
// entry that would resolve it (FR-056). The same file, ignored, is accepted
// with no warning: a value that cannot be committed is not in the repository.
//
// The check runs at load time, before any value is read from the file and
// before any platform call (FR-057). It has no bypass.
func CheckNotInRepository(ctx context.Context, repoRoot, path string) error {
	if repoRoot == "" {
		return nil
	}

	inside, err := gitrepo.IsInside(repoRoot, path)
	if err != nil {
		return fmt.Errorf("locating %s relative to the working copy: %w", path, err)
	}
	if !inside {
		return nil
	}

	ignored, err := gitrepo.IsIgnored(ctx, repoRoot, path)
	if err != nil {
		return fmt.Errorf("asking git whether %s is ignored: %w", path, err)
	}
	if ignored {
		return nil
	}

	return fmt.Errorf(
		"%w: %s lies inside the working copy and git does not ignore it; "+
			"add it to .gitignore before rerunning (this refusal has no bypass)",
		ErrValuesInRepo, path)
}
