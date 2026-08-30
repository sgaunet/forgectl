package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sgaunet/forgectl/internal/config"
)

// load parses a configuration body and returns the resolved tree.
func load(t *testing.T, body string) *config.Config {
	t.Helper()

	resolved, err := config.Load(
		config.Options{Path: writeConfig(t, body), PathSet: true},
		config.Environment{},
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	return &resolved.Config
}

func TestSelectProfilesFromArguments(t *testing.T) {
	cfg := load(t, `
values:
  a: value-a
  b: value-b
profiles:
  one:
    protected_tags: ["v*"]
    variables:
      - name: A
        value_ref: a
  two:
    protected_tags: ["release-*"]
    variables:
      - name: B
        value_ref: b
`)

	sel, err := config.SelectProfiles(cfg, []string{"one", "two"}, "")
	if err != nil {
		t.Fatalf("SelectProfiles: %v", err)
	}

	if len(sel.Names) != 2 {
		t.Errorf("names = %v, want both profiles", sel.Names)
	}
	if len(sel.Variables) != 2 {
		t.Errorf("variables = %d, want 2 (the union)", len(sel.Variables))
	}
	// FR-014, FR-025: the tag patterns union too.
	if len(sel.ProtectedTags) != 2 {
		t.Errorf("protected tags = %v, want both patterns", sel.ProtectedTags)
	}
}

func TestSelectProfilesFallsBackToTheRepositoryFile(t *testing.T) {
	cfg := load(t, `
values:
  a: value-a
profiles:
  one:
    variables:
      - name: A
        value_ref: a
`)

	repo := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(repo, config.RepoFileName),
		[]byte("profiles:\n  - one\n"), 0o600,
	); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// FR-017: with no name on the command line, the repository file supplies it.
	sel, err := config.SelectProfiles(cfg, nil, repo)
	if err != nil {
		t.Fatalf("SelectProfiles: %v", err)
	}
	if len(sel.Names) != 1 || sel.Names[0] != "one" {
		t.Errorf("names = %v, want [one] from %s", sel.Names, config.RepoFileName)
	}

	// An argument beats the repository file.
	cfg.Profiles["two"] = config.Profile{Name: "two"}
	sel, err = config.SelectProfiles(cfg, []string{"two"}, repo)
	if err != nil {
		t.Fatalf("SelectProfiles: %v", err)
	}
	if len(sel.Names) != 1 || sel.Names[0] != "two" {
		t.Errorf("names = %v, want [two]: an argument beats the repository file", sel.Names)
	}
}

func TestNoProfileSelectedAnywhere(t *testing.T) {
	cfg := load(t, "settings:\n  default_branch: main\n")

	sel, err := config.SelectProfiles(cfg, nil, t.TempDir())
	if err != nil {
		t.Fatalf("SelectProfiles: %v", err)
	}
	// FR-019: no profile selected means only branch and protection run.
	if !sel.Empty() {
		t.Errorf("selection = %v, want empty", sel.Names)
	}
}

func TestIdenticalVariableInTwoProfilesIsHandledOnce(t *testing.T) {
	// FR-018 and US3 acceptance scenario 1.
	cfg := load(t, `
values:
  shared: value
profiles:
  one:
    variables:
      - name: SHARED
        value_ref: shared
        secret: true
        masked: true
  two:
    variables:
      - name: SHARED
        value_ref: shared
        secret: true
        masked: true
`)

	sel, err := config.SelectProfiles(cfg, []string{"one", "two"}, "")
	if err != nil {
		t.Fatalf("SelectProfiles: %v", err)
	}
	if len(sel.Variables) != 1 {
		t.Errorf("variables = %d, want 1: an identical declaration is handled exactly once",
			len(sel.Variables))
	}
}

func TestConflictingVariableAcrossProfilesIsRefused(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "differing attributes",
			body: `
values:
  shared: value
profiles:
  one:
    variables:
      - name: SHARED
        value_ref: shared
        masked: true
  two:
    variables:
      - name: SHARED
        value_ref: shared
        masked: false
`,
		},
		{
			name: "differing value sources",
			body: `
values:
  a: value-a
  b: value-b
profiles:
  one:
    variables:
      - name: SHARED
        value_ref: a
  two:
    variables:
      - name: SHARED
        value_ref: b
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := load(t, tt.body)

			_, err := config.SelectProfiles(cfg, []string{"one", "two"}, "")
			if err == nil {
				t.Fatal("a conflicting declaration was accepted")
			}
			if !config.ValidationProblems(err) {
				t.Fatalf("error %v does not wrap ErrInvalid", err)
			}

			// FR-018: the error names the variable and both profiles, and is
			// raised before any platform call.
			msg := err.Error()
			for _, want := range []string{"SHARED", "one", "two"} {
				if !strings.Contains(msg, want) {
					t.Errorf("message %q does not name %q", msg, want)
				}
			}
		})
	}
}

func TestUnknownProfileNameIsRefused(t *testing.T) {
	cfg := load(t, "settings:\n  default_branch: main\n")

	_, err := config.SelectProfiles(cfg, []string{"no-such-profile"}, "")
	if err == nil {
		t.Fatal("an unknown profile name was accepted")
	}
	if !errors.Is(err, config.ErrUnknownProfile) {
		t.Errorf("error %v does not wrap ErrUnknownProfile", err)
	}
}

func TestProfileNamesAreListedOnce(t *testing.T) {
	// FR-020 and US5 acceptance scenario 2: a configured profile whose name
	// matches a built-in one appears once, reflecting the configured definition.
	cfg := load(t, `
values:
  k: v
profiles:
  go-release:
    variables:
      - name: OVERRIDDEN
        value_ref: k
`)

	names := config.ProfileNames(cfg)
	seen := 0
	for _, n := range names {
		if n == "go-release" {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("go-release appears %d times in %v, want once", seen, names)
	}

	profile, err := config.LookupProfile(cfg, "go-release")
	if err != nil {
		t.Fatalf("LookupProfile: %v", err)
	}
	if len(profile.Variables) != 1 || profile.Variables[0].Name != "OVERRIDDEN" {
		t.Errorf("the listing does not reflect the configured definition: %+v", profile.Variables)
	}
}

func TestDuplicateProfileArgumentsAreDeduplicated(t *testing.T) {
	cfg := load(t, `
values:
  k: v
profiles:
  one:
    variables:
      - name: A
        value_ref: k
`)

	sel, err := config.SelectProfiles(cfg, []string{"one", "one"}, "")
	if err != nil {
		t.Fatalf("SelectProfiles: %v", err)
	}
	if len(sel.Names) != 1 {
		t.Errorf("names = %v, want the duplicate removed", sel.Names)
	}
}
