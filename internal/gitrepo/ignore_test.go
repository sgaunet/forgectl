package gitrepo_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sgaunet/forgectl/internal/gitrepo"
)

func TestIsIgnored(t *testing.T) {
	dir := newRepo(t, "main", "https://github.com/sgaunet/forgectl.git")

	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("vars.yaml\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ignored := filepath.Join(dir, "vars.yaml")
	tracked := filepath.Join(dir, "config.yaml")
	for _, p := range []string{ignored, tracked} {
		if err := os.WriteFile(p, []byte("values: {}\n"), 0o600); err != nil {
			t.Fatalf("WriteFile %s: %v", p, err)
		}
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "listed in .gitignore", path: ignored, want: true},
		{name: "not ignored", path: tracked, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := gitrepo.IsIgnored(context.Background(), dir, tt.path)
			if err != nil {
				t.Fatalf("IsIgnored: %v", err)
			}
			if got != tt.want {
				t.Errorf("IsIgnored(%s) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestIsInsideWorkingCopy(t *testing.T) {
	dir := newRepo(t, "main", "https://github.com/sgaunet/forgectl.git")

	sub := filepath.Join(dir, "nested")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	outside := t.TempDir()

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "at the root", path: filepath.Join(dir, "vars.yaml"), want: true},
		{name: "in a subdirectory", path: filepath.Join(sub, "vars.yaml"), want: true},
		{name: "outside the working copy entirely", path: filepath.Join(outside, "vars.yaml"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := gitrepo.IsInside(dir, tt.path)
			if err != nil {
				t.Fatalf("IsInside: %v", err)
			}
			if got != tt.want {
				t.Errorf("IsInside(%s) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestIsIgnoredOutsideTheWorkingCopy(t *testing.T) {
	dir := newRepo(t, "main", "https://github.com/sgaunet/forgectl.git")
	outside := filepath.Join(t.TempDir(), "vars.yaml")

	// A path git cannot reason about must not be reported as ignored: the
	// caller decides what to do with a file outside the working copy, and
	// FR-056 only concerns files inside it.
	got, err := gitrepo.IsIgnored(context.Background(), dir, outside)
	if err != nil {
		t.Fatalf("IsIgnored: %v", err)
	}
	if got {
		t.Error("IsIgnored reported a path outside the working copy as ignored")
	}
}
