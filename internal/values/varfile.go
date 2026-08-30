package values

import (
	"context"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/sgaunet/forgectl/internal/config"
)

// VarFile is the one-off, per-repository override file named by --var-file. It
// is the highest-precedence source in the resolution chain of FR-044.
type VarFile struct {
	Values map[string]string `yaml:"values"`
	Path   string            `yaml:"-"`
}

// LoadVarFile reads an override file, refusing one that lies inside the working
// copy while git does not ignore it (FR-056).
//
// The refusal happens BEFORE the file is opened, let alone parsed (FR-057): a
// values file one `git add` away from being published is a problem whether or
// not forgectl has read it yet, and reading it first would be the tool putting
// the value in its own memory to tell you it should not exist there.
//
// This refusal has no bypass. --allow-insecure-config lifts the permission
// check of FR-007 and nothing else.
func LoadVarFile(ctx context.Context, path, repoRoot string) (*VarFile, error) {
	if path == "" {
		return &VarFile{Values: map[string]string{}}, nil
	}

	if err := config.CheckNotInRepository(ctx, repoRoot, path); err != nil {
		return nil, err //nolint:wrapcheck // the sentinel must stay reachable for classify
	}

	// The file holds values, so its permissions are enforced exactly as the
	// configuration file's are (FR-007).
	if err := config.CheckPermissions(path, false); err != nil {
		return nil, fmt.Errorf("the override file: %w", err)
	}

	data, err := os.ReadFile(path) //nolint:gosec // the path is the maintainer's own file
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	file := &VarFile{Path: path, Values: map[string]string{}}
	if err := yaml.Unmarshal(data, file); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", config.ErrInvalid, path, err)
	}

	if file.Values == nil {
		file.Values = map[string]string{}
	}

	return file, nil
}
