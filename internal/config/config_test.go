package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sgaunet/forgectl/internal/config"
)

// writeConfig writes a configuration file at mode 0600 and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	return path
}

func TestDefaultsWithNoConfigurationFile(t *testing.T) {
	// A maintainer with no configuration file at all must still be able to work
	// against the two public hosts (SC-001).
	resolved, err := config.Load(config.Options{
		Path:    filepath.Join(t.TempDir(), "absent.yaml"),
		PathSet: true,
	}, config.Environment{})
	if err != nil {
		t.Fatalf("Load with no file: %v", err)
	}

	if got := resolved.Config.Settings.DefaultBranch; got != "main" {
		t.Errorf("default branch = %q, want main (FR-015)", got)
	}
	if got := resolved.Remote; got != "origin" {
		t.Errorf("remote = %q, want origin", got)
	}
	if got := resolved.Output; got != "text" {
		t.Errorf("output = %q, want text", got)
	}

	bp := resolved.Config.BranchProtection
	if !bp.Enabled || bp.AllowForcePush || bp.AllowDelete || bp.PushAccessLevel != config.AccessMaintainer {
		t.Errorf("branch protection defaults = %+v, want enabled with force-push and deletion denied "+
			"and push restricted to maintainers (FR-015)", bp)
	}

	if len(resolved.Config.Profiles) != 3 {
		t.Errorf("profiles = %d, want the 3 built-in ones (FR-008)", len(resolved.Config.Profiles))
	}
	for _, name := range []string{"ansible-role", "go-release", "ssh-deploy"} {
		if _, ok := resolved.Config.Profiles[name]; !ok {
			t.Errorf("built-in profile %q is missing", name)
		}
	}
}

func TestPrecedenceFlagsBeatEnvironmentBeatsDefaults(t *testing.T) {
	path := writeConfig(t, "settings:\n  default_branch: trunk\n")

	tests := []struct {
		name       string
		opts       config.Options
		env        config.Environment
		wantRemote string
		wantOutput string
	}{
		{
			name:       "defaults only",
			opts:       config.Options{Path: path, PathSet: true},
			wantRemote: "origin",
			wantOutput: "text",
		},
		{
			name:       "environment overrides the default",
			opts:       config.Options{Path: path, PathSet: true},
			env:        config.Environment{Remote: "upstream", Output: "json"},
			wantRemote: "upstream",
			wantOutput: "json",
		},
		{
			name: "a set flag beats the environment",
			opts: config.Options{
				Path: path, PathSet: true,
				Remote: "fork", RemoteSet: true,
				Output: "text", OutputSet: true,
			},
			env:        config.Environment{Remote: "upstream", Output: "json"},
			wantRemote: "fork",
			wantOutput: "text",
		},
		{
			name: "an unset flag yields to the environment even when it carries a value",
			opts: config.Options{
				Path: path, PathSet: true,
				Remote: "ignored-because-unset", RemoteSet: false,
			},
			env:        config.Environment{Remote: "upstream"},
			wantRemote: "upstream",
			wantOutput: "text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, err := config.Load(tt.opts, tt.env)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if resolved.Remote != tt.wantRemote {
				t.Errorf("remote = %q, want %q", resolved.Remote, tt.wantRemote)
			}
			if resolved.Output != tt.wantOutput {
				t.Errorf("output = %q, want %q", resolved.Output, tt.wantOutput)
			}
			if got := resolved.Config.Settings.DefaultBranch; got != "trunk" {
				t.Errorf("the file layer did not apply: default branch = %q, want trunk", got)
			}
		})
	}
}

func TestConfigPathFromEnvironment(t *testing.T) {
	path := writeConfig(t, "settings:\n  default_branch: release\n")

	resolved, err := config.Load(config.Options{}, config.Environment{Path: path})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := resolved.Config.Settings.DefaultBranch; got != "release" {
		t.Errorf("FORGECTL_CONFIG was not honoured: default branch = %q", got)
	}

	// A set --config flag beats FORGECTL_CONFIG.
	other := writeConfig(t, "settings:\n  default_branch: from-flag\n")
	resolved, err = config.Load(
		config.Options{Path: other, PathSet: true},
		config.Environment{Path: path},
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := resolved.Config.Settings.DefaultBranch; got != "from-flag" {
		t.Errorf("--config did not beat FORGECTL_CONFIG: default branch = %q", got)
	}
}

func TestConfiguredProfileReplacesTheBuiltinEntirely(t *testing.T) {
	// A partial override must not silently inherit a variable the maintainer
	// meant to drop (FR-008).
	path := writeConfig(t, `
values:
  token: placeholder
profiles:
  go-release:
    variables:
      - name: ONLY_THIS_ONE
        value_ref: token
`)

	resolved, err := config.Load(config.Options{Path: path, PathSet: true}, config.Environment{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	profile := resolved.Config.Profiles["go-release"]
	if len(profile.Variables) != 1 || profile.Variables[0].Name != "ONLY_THIS_ONE" {
		t.Fatalf("configured profile did not replace the built-in one: %+v", profile.Variables)
	}
	if len(profile.ProtectedTags) != 0 {
		t.Errorf("the built-in protected_tags leaked into the override: %v", profile.ProtectedTags)
	}

	// The profiles it did not name are untouched.
	if _, ok := resolved.Config.Profiles["ansible-role"]; !ok {
		t.Error("overriding one profile dropped the others")
	}
}

func TestConfiguredProfileExtendsTheSet(t *testing.T) {
	path := writeConfig(t, `
values:
  token: placeholder
profiles:
  python-package:
    variables:
      - name: PYPI_TOKEN
        value_ref: token
`)

	resolved, err := config.Load(config.Options{Path: path, PathSet: true}, config.Environment{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(resolved.Config.Profiles) != 4 {
		t.Errorf("profiles = %d, want 4 (3 built-in plus 1 configured)", len(resolved.Config.Profiles))
	}
	if _, ok := resolved.Config.Profiles["python-package"]; !ok {
		t.Error("the configured profile did not extend the set")
	}
}

func TestVariableAttributeDefaults(t *testing.T) {
	path := writeConfig(t, `
values:
  token: placeholder
profiles:
  demo:
    variables:
      - name: DEFAULTED
        value_ref: token
      - name: EXPLICIT
        value_ref: token
        secret: false
        masked: true
        protected: true
`)

	resolved, err := config.Load(config.Options{Path: path, PathSet: true}, config.Environment{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	vars := map[string]config.VariableDefinition{}
	for _, v := range resolved.Config.Profiles["demo"].Variables {
		vars[v.Name] = v
	}

	// FR-011: secret defaults to true, masked and protected to false.
	if d := vars["DEFAULTED"]; !d.Secret || d.Masked || d.Protected {
		t.Errorf("defaults = secret:%v masked:%v protected:%v, want true/false/false (FR-011)",
			d.Secret, d.Masked, d.Protected)
	}

	// An explicit false must survive, which is why the parse distinguishes
	// "absent" from "set to the zero value".
	if e := vars["EXPLICIT"]; e.Secret || !e.Masked || !e.Protected {
		t.Errorf("explicit = secret:%v masked:%v protected:%v, want false/true/true",
			e.Secret, e.Masked, e.Protected)
	}
}

func TestGeneratorDefaults(t *testing.T) {
	path := writeConfig(t, `
profiles:
  demo:
    variables:
      - name: TOKEN
        generator: gitlab-pat
`)

	resolved, err := config.Load(config.Options{Path: path, PathSet: true}, config.Environment{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	gen := resolved.Config.Profiles["demo"].Variables[0].Generator
	if gen == nil {
		t.Fatal("the generator was not parsed")
	}

	// FR-012.
	if gen.TokenName != "forgectl" {
		t.Errorf("token_name = %q, want forgectl", gen.TokenName)
	}
	if len(gen.Scopes) != 1 || gen.Scopes[0] != "api" {
		t.Errorf("scopes = %v, want [api]", gen.Scopes)
	}
	if gen.Role != config.AccessMaintainer {
		t.Errorf("role = %q, want maintainer", gen.Role)
	}
	if gen.ExpiresIn != config.Days(180) {
		t.Errorf("expires_in = %s, want 180d", gen.ExpiresIn)
	}
	if gen.RotateBefore != config.Days(60) {
		t.Errorf("rotate_before = %s, want 60d", gen.RotateBefore)
	}
	if !gen.RevokeRotated {
		t.Error("revoke_rotated = false, want true")
	}
}

func TestParseDays(t *testing.T) {
	good := map[string]config.Days{"0d": 0, "1d": 1, "180d": 180, "365d": 365}
	for in, want := range good {
		got, err := config.ParseDays(in)
		if err != nil {
			t.Errorf("ParseDays(%q): %v", in, err)

			continue
		}
		if got != want {
			t.Errorf("ParseDays(%q) = %d, want %d", in, got, want)
		}
		if rendered := got.String(); rendered != in {
			t.Errorf("Days(%d).String() = %q, want %q", got, rendered, in)
		}
	}

	// FR-013: any other form is a configuration error.
	for _, in := range []string{"", "d", "180", "180D", "6m", "1y", "24h", "-5d", "1.5d"} {
		if _, err := config.ParseDays(in); err == nil {
			t.Errorf("ParseDays(%q) succeeded, want a configuration error", in)
		}
	}
}

func TestParsePlatformAndAccessLevel(t *testing.T) {
	if _, err := config.ParsePlatform("gitea"); err == nil {
		t.Error("ParsePlatform accepted an unknown platform")
	}
	if _, err := config.ParseAccessLevel("owner"); err == nil {
		t.Error("ParseAccessLevel accepted an unknown level")
	}

	levels := map[config.AccessLevel]int{
		config.AccessNone:       0,
		config.AccessDeveloper:  30,
		config.AccessMaintainer: 40,
	}
	for level, want := range levels {
		if got := level.GitLab(); got != want {
			t.Errorf("%s.GitLab() = %d, want %d", level, got, want)
		}
	}
}

func TestUnknownOutputFormatIsRejected(t *testing.T) {
	_, err := config.Load(
		config.Options{Output: "yaml", OutputSet: true},
		config.Environment{},
	)
	if err == nil {
		t.Fatal("Load accepted --output=yaml")
	}
	if !config.ValidationProblems(err) {
		t.Errorf("error %v does not wrap ErrInvalid", err)
	}
}
