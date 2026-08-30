package report_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sgaunet/forgectl/internal/config"
	"github.com/sgaunet/forgectl/internal/report"
)

// loadConfig parses a configuration body and returns the resolved tree.
func loadConfig(t *testing.T, body string) *config.Config {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	resolved, err := config.Load(
		config.Options{Path: path, PathSet: true}, config.Environment{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	return &resolved.Config
}

func TestListingWithNoConfigurationShowsTheThreeBuiltins(t *testing.T) {
	// US5 acceptance scenario 1, and SC-001: the three built-in profiles are
	// available with no configuration file at all.
	cfg := loadConfig(t, "settings:\n  default_branch: main\n")

	listing := report.ListingOf(cfg)

	if len(listing.Profiles) != 3 {
		t.Fatalf("profiles = %d, want the 3 built-in ones", len(listing.Profiles))
	}

	seen := map[string]string{}
	for _, p := range listing.Profiles {
		seen[p.Name] = p.Source
	}

	for _, name := range []string{"ansible-role", "go-release", "ssh-deploy"} {
		source, present := seen[name]
		if !present {
			t.Errorf("the built-in profile %q is missing", name)

			continue
		}
		if source != report.SourceBuiltin {
			t.Errorf("%s is reported as %q, want builtin", name, source)
		}
	}
}

func TestAConfiguredProfileOfABuiltinNameAppearsOnce(t *testing.T) {
	// US5 acceptance scenario 2: the name appears once and reflects the
	// configured definition.
	cfg := loadConfig(t, `
values:
  k: v
profiles:
  go-release:
    variables:
      - name: OVERRIDDEN
        value_ref: k
`)

	listing := report.ListingOf(cfg)

	var count int
	var summary report.ProfileSummary

	for _, p := range listing.Profiles {
		if p.Name == "go-release" {
			count++
			summary = p
		}
	}

	if count != 1 {
		t.Fatalf("go-release appears %d times, want once", count)
	}
	if summary.Source != report.SourceConfigured {
		t.Errorf("source = %q, want configured: the maintainer's definition is in force",
			summary.Source)
	}
	if summary.Variables != 1 {
		t.Errorf("variables = %d, want the configured definition's 1", summary.Variables)
	}
}

func TestDetailShowsAttributesAndValueSourceKinds(t *testing.T) {
	// FR-020: each variable's name, attributes, value-source kind, and the
	// profile's protected tag patterns.
	cfg := loadConfig(t, "settings:\n  default_branch: main\n")

	profile, err := config.LookupProfile(cfg, "go-release")
	if err != nil {
		t.Fatalf("LookupProfile: %v", err)
	}

	detail := report.DetailOf(cfg, profile, "go-release")

	if len(detail.ProtectedTags) == 0 {
		t.Error("the detail shows no protected tag patterns")
	}
	if len(detail.Variables) != 1 {
		t.Fatalf("variables = %d, want 1", len(detail.Variables))
	}

	v := detail.Variables[0]
	if v.Name != "GITLAB_TOKEN" {
		t.Errorf("name = %q", v.Name)
	}
	if v.ValueSource != config.SourceGenerator.String() {
		t.Errorf("value_source = %q, want generator", v.ValueSource)
	}
	if v.Generator != config.GeneratorKindGitLabPAT {
		t.Errorf("generator = %q, want gitlab-pat", v.Generator)
	}
	if !v.Secret || !v.Masked || !v.Protected {
		t.Errorf("attributes = secret:%v masked:%v protected:%v, want the declared ones",
			v.Secret, v.Masked, v.Protected)
	}
}

func TestValueSourceKindsAreDistinguished(t *testing.T) {
	cfg := loadConfig(t, `
values:
  k: some-value
profiles:
  mixed:
    variables:
      - name: FROM_LITERAL
        value: inline-value
      - name: FROM_REF
        value_ref: k
      - name: FROM_GENERATOR
        generator: gitlab-pat
`)

	profile, err := config.LookupProfile(cfg, "mixed")
	if err != nil {
		t.Fatalf("LookupProfile: %v", err)
	}

	detail := report.DetailOf(cfg, profile, "mixed")

	want := map[string]string{
		"FROM_LITERAL":   config.SourceLiteral.String(),
		"FROM_REF":       config.SourceRef.String(),
		"FROM_GENERATOR": config.SourceGenerator.String(),
	}

	for _, v := range detail.Variables {
		if got := v.ValueSource; got != want[v.Name] {
			t.Errorf("%s: value_source = %q, want %q", v.Name, got, want[v.Name])
		}
	}
}

func TestTheDetailShowsNoValue(t *testing.T) {
	// FR-020 and SC-009: a maintainer who did not write the configuration can
	// learn where each value comes from without being shown one.
	const (
		inline = "SENTINEL-inline-value"
		stored = "SENTINEL-stored-value"
	)

	cfg := loadConfig(t, `
values:
  k: `+stored+`
profiles:
  mixed:
    variables:
      - name: FROM_LITERAL
        value: `+inline+`
      - name: FROM_REF
        value_ref: k
`)

	profile, err := config.LookupProfile(cfg, "mixed")
	if err != nil {
		t.Fatalf("LookupProfile: %v", err)
	}

	detail := report.DetailOf(cfg, profile, "mixed")

	var rendered strings.Builder
	if err := report.WriteDetailText(&rendered, detail); err != nil {
		t.Fatalf("WriteDetailText: %v", err)
	}

	for _, sentinel := range []string{inline, stored} {
		if strings.Contains(rendered.String(), sentinel) {
			t.Errorf("the detail view shows a value:\n%s", rendered.String())
		}
	}

	// The key of a reference is a name, not a value, and is safe to show —
	// but the detail deliberately shows only the KIND, so a reader learns
	// "this comes from the value store" without learning which key.
	if strings.Contains(rendered.String(), stored) {
		t.Error("the detail view shows the stored value")
	}
}

func TestListingTextIsTabular(t *testing.T) {
	cfg := loadConfig(t, "settings:\n  default_branch: main\n")

	var rendered strings.Builder
	if err := report.WriteListingText(&rendered, report.ListingOf(cfg)); err != nil {
		t.Fatalf("WriteListingText: %v", err)
	}

	out := rendered.String()
	for _, want := range []string{"PROFILE", "SOURCE", "ansible-role", "go-release", "ssh-deploy"} {
		if !strings.Contains(out, want) {
			t.Errorf("the listing omits %q:\n%s", want, out)
		}
	}
}
