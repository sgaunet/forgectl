package main_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfilesListWithNoConfigurationFileAtAll(t *testing.T) {
	// US5 independent test, and quickstart Scenario 8: with no configuration
	// file the three built-in profiles appear.
	//
	// HOME points at an empty directory so the developer's own
	// ~/.config/forgectl/config.yaml cannot influence the result.
	home := t.TempDir()

	got := forgectl(t, t.TempDir(), []string{"HOME=" + home, "XDG_CONFIG_HOME=" + home}, "profiles", "list")

	if got.code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr: %s", got.code, got.stderr)
	}

	for _, name := range []string{"ansible-role", "go-release", "ssh-deploy"} {
		if !strings.Contains(got.stdout, name) {
			t.Errorf("the listing omits the built-in profile %q:\n%s", name, got.stdout)
		}
	}
}

func TestProfilesWorkOutsideAWorkingCopy(t *testing.T) {
	// Someone deciding whether to adopt a profile has no reason to be standing
	// in a repository first.
	home := t.TempDir()
	env := []string{"HOME=" + home, "XDG_CONFIG_HOME=" + home}

	for _, args := range [][]string{
		{"profiles", "list"},
		{"profiles", "show", "go-release"},
	} {
		got := forgectl(t, t.TempDir(), env, args...)
		if got.code != 0 {
			t.Errorf("%v outside a working copy: exit %d\nstderr: %s", args, got.code, got.stderr)
		}
	}
}

func TestProfilesShowDisclosesNoValue(t *testing.T) {
	// FR-020, SC-009.
	const (
		inline = "SENTINEL-profiles-inline"
		stored = "SENTINEL-profiles-stored"
	)

	body := `
settings:
  default_branch: main
values:
  k: ` + stored + `
profiles:
  mixed:
    protected_tags:
      - "v*"
    variables:
      - name: FROM_LITERAL
        value: ` + inline + `
      - name: FROM_REF
        value_ref: k
`

	cfg := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	for _, args := range [][]string{
		{"profiles", "show", "mixed"},
		{"profiles", "show", "mixed", "--output=json"},
		{"profiles", "list"},
	} {
		got := forgectl(t, t.TempDir(), []string{"FORGECTL_CONFIG=" + cfg}, args...)

		if got.code != 0 {
			t.Fatalf("%v: exit %d\nstderr: %s", args, got.code, got.stderr)
		}

		for _, sentinel := range []string{inline, stored} {
			if strings.Contains(got.stdout+got.stderr, sentinel) {
				t.Errorf("%v disclosed a value:\nstdout: %s\nstderr: %s",
					args, got.stdout, got.stderr)
			}
		}
	}
}

func TestProfilesShowReportsTheValueSourceKind(t *testing.T) {
	home := t.TempDir()

	got := forgectl(t, t.TempDir(),
		[]string{"HOME=" + home, "XDG_CONFIG_HOME=" + home},
		"profiles", "show", "go-release", "--output=json")

	if got.code != 0 {
		t.Fatalf("exit = %d\nstderr: %s", got.code, got.stderr)
	}

	var detail struct {
		Name          string   `json:"name"`
		ProtectedTags []string `json:"protected_tags"`
		Variables     []struct {
			Name        string `json:"name"`
			ValueSource string `json:"value_source"`
			Generator   string `json:"generator"`
		} `json:"variables"`
	}
	if err := json.Unmarshal([]byte(got.stdout), &detail); err != nil {
		t.Fatalf("stdout does not parse: %v\n%s", err, got.stdout)
	}

	if detail.Name != "go-release" {
		t.Errorf("name = %q", detail.Name)
	}
	if len(detail.ProtectedTags) == 0 {
		t.Error("the detail carries no protected tag patterns")
	}
	if len(detail.Variables) == 0 {
		t.Fatal("the detail carries no variables")
	}

	v := detail.Variables[0]
	if v.ValueSource != "generator" || v.Generator != "gitlab-pat" {
		t.Errorf("value_source = %q, generator = %q, want generator / gitlab-pat",
			v.ValueSource, v.Generator)
	}
}

func TestAnUnknownProfileNameExitsTwo(t *testing.T) {
	home := t.TempDir()

	got := forgectl(t, t.TempDir(),
		[]string{"HOME=" + home, "XDG_CONFIG_HOME=" + home},
		"profiles", "show", "no-such-profile")

	if got.code != 2 {
		t.Errorf("exit = %d, want 2\nstderr: %s", got.code, got.stderr)
	}
	if !strings.Contains(got.stderr, "profiles list") {
		t.Errorf("stderr does not say how to find the available profiles:\n%s", got.stderr)
	}
}

func TestProfilesAreReadOnlyAndNeverPrompt(t *testing.T) {
	// CLI-003: detect, check, profiles, and version are read-only and require
	// no confirmation. With stdin not a terminal, a command that prompted would
	// exit 2; these must not.
	home := t.TempDir()
	env := []string{"HOME=" + home, "XDG_CONFIG_HOME=" + home}

	for _, args := range [][]string{
		{"profiles", "list"},
		{"profiles", "show", "ssh-deploy"},
	} {
		if got := forgectl(t, t.TempDir(), env, args...); got.code != 0 {
			t.Errorf("%v: exit %d, want 0\nstderr: %s", args, got.code, got.stderr)
		}
	}
}
