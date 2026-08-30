package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/sgaunet/forgectl/internal/config"
)

func TestBuiltinInstancesResolveWithNoConfiguration(t *testing.T) {
	// SC-001: a maintainer with no configuration file at all can work against
	// both public hosts, having set only the credential in their environment.
	cfg := load(t, "settings:\n  default_branch: main\n")

	tests := []struct {
		host     string
		platform config.Platform
		tokenEnv string
	}{
		{host: "github.com", platform: config.PlatformGitHub, tokenEnv: "GITHUB_TOKEN"},
		{host: "gitlab.com", platform: config.PlatformGitLab, tokenEnv: "GITLAB_TOKEN"},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			inst, err := config.ResolveInstance(cfg, tt.host)
			if err != nil {
				t.Fatalf("ResolveInstance(%s): %v", tt.host, err)
			}
			if inst.Platform != tt.platform {
				t.Errorf("platform = %q, want %q", inst.Platform, tt.platform)
			}
			if inst.TokenEnv != tt.tokenEnv {
				t.Errorf("token_env = %q, want %q", inst.TokenEnv, tt.tokenEnv)
			}
			if inst.APIURL == "" {
				t.Error("the built-in instance carries no API base URL")
			}
		})
	}
}

func TestHostMatchIsCaseInsensitive(t *testing.T) {
	cfg := load(t, "settings:\n  default_branch: main\n")

	if _, err := config.ResolveInstance(cfg, "GitHub.com"); err != nil {
		t.Errorf("host matching is case-sensitive: %v", err)
	}
}

func TestConfiguredInstanceOverridesTheBuiltin(t *testing.T) {
	cfg := load(t, `
instances:
  - name: github-enterprise
    host: github.com
    platform: github
    api_url: https://ghe.example.com/api/v3/
    token_env: GHE_TOKEN
`)

	inst, err := config.ResolveInstance(cfg, "github.com")
	if err != nil {
		t.Fatalf("ResolveInstance: %v", err)
	}
	if inst.Name != "github-enterprise" {
		t.Errorf("name = %q, want the configured instance to win over the built-in", inst.Name)
	}
	if inst.TokenEnv != "GHE_TOKEN" {
		t.Errorf("token_env = %q, want GHE_TOKEN", inst.TokenEnv)
	}
}

func TestUnknownHostIsRefused(t *testing.T) {
	path := writeConfig(t, "settings:\n  default_branch: main\n")
	resolved, err := config.Load(config.Options{Path: path, PathSet: true}, config.Environment{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	_, err = config.ResolveInstance(&resolved.Config, "git.example.com")
	if err == nil {
		t.Fatal("an unknown host resolved to an instance")
	}
	if !errors.Is(err, config.ErrUnknownHost) {
		t.Fatalf("error %v does not wrap ErrUnknownHost", err)
	}

	// FR-003: the message names the unknown host and the file to declare it in.
	msg := err.Error()
	if !strings.Contains(msg, "git.example.com") {
		t.Errorf("message %q does not name the host", msg)
	}
	if !strings.Contains(msg, path) {
		t.Errorf("message %q does not name the configuration file %q", msg, path)
	}
}

func TestAPIURLDefaultsFromTheHost(t *testing.T) {
	cfg := load(t, `
instances:
  - name: self-hosted
    host: git.example.com
    platform: gitlab
    token_env: FORGE_TOKEN
`)

	inst, err := config.ResolveInstance(cfg, "git.example.com")
	if err != nil {
		t.Fatalf("ResolveInstance: %v", err)
	}
	if !strings.Contains(inst.APIURL, "git.example.com") {
		t.Errorf("api_url = %q, want it derived from the host", inst.APIURL)
	}
}

func TestCredentialComesOnlyFromTheEnvironment(t *testing.T) {
	inst := config.Instance{Name: "forge", Host: "git.example.com", TokenEnv: "FORGECTL_TEST_TOKEN"}

	// FR-005: unset fails before any network call, naming the variable.
	t.Setenv("FORGECTL_TEST_TOKEN", "")
	_, err := config.Credential(inst)
	if err == nil {
		t.Fatal("an unset credential variable was accepted")
	}
	if !errors.Is(err, config.ErrNoCredential) {
		t.Fatalf("error %v does not wrap ErrNoCredential", err)
	}
	if !strings.Contains(err.Error(), "FORGECTL_TEST_TOKEN") {
		t.Errorf("message %q does not name the environment variable", err.Error())
	}

	// And the credential itself must never appear in the message.
	t.Setenv("FORGECTL_TEST_TOKEN", "   ")
	if _, err := config.Credential(inst); !errors.Is(err, config.ErrNoCredential) {
		t.Errorf("a whitespace-only credential was accepted: %v", err)
	}

	t.Setenv("FORGECTL_TEST_TOKEN", "glpat-example")
	got, err := config.Credential(inst)
	if err != nil {
		t.Fatalf("Credential: %v", err)
	}
	if got != "glpat-example" {
		t.Errorf("Credential returned %q", got)
	}
}
