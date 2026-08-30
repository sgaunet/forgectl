package config_test

import (
	"strings"
	"testing"

	"github.com/sgaunet/forgectl/internal/config"
)

// loadInvalid loads a configuration expected to be refused, and returns the
// message so the test can check it names the offending element.
func loadInvalid(t *testing.T, body string) string {
	t.Helper()

	_, err := config.Load(config.Options{Path: writeConfig(t, body), PathSet: true}, config.Environment{})
	if err == nil {
		t.Fatal("Load accepted an invalid configuration")
	}
	if !config.ValidationProblems(err) {
		t.Fatalf("error %v does not wrap ErrInvalid", err)
	}

	return err.Error()
}

func TestVariableMustDeclareExactlyOneValueSource(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "no source at all",
			body: "profiles:\n  demo:\n    variables:\n      - name: TOKEN\n",
			want: "declares no value source",
		},
		{
			name: "a literal and a reference",
			body: "values:\n  k: v\nprofiles:\n  demo:\n    variables:\n" +
				"      - name: TOKEN\n        value: inline\n        value_ref: k\n",
			want: "declares more than one value source",
		},
		{
			name: "a reference and a generator",
			body: "values:\n  k: v\nprofiles:\n  demo:\n    variables:\n" +
				"      - name: TOKEN\n        value_ref: k\n        generator: gitlab-pat\n",
			want: "declares more than one value source",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := loadInvalid(t, tt.body)
			if !strings.Contains(msg, tt.want) {
				t.Errorf("message %q does not report %q", msg, tt.want)
			}
			// FR-009: the message must name the variable.
			if !strings.Contains(msg, "TOKEN") {
				t.Errorf("message %q does not name the variable", msg)
			}
		})
	}
}

func TestUnresolvedValueRefIsRefusedWhenTheProfileIsSelected(t *testing.T) {
	// FR-010. The reference is judged once the run knows which profiles it
	// uses, so an unused profile's reference cannot refuse to start.
	path := writeConfig(t, `
profiles:
  demo:
    variables:
      - name: TOKEN
        value_ref: nowhere
`)

	resolved, err := config.Load(config.Options{Path: path, PathSet: true}, config.Environment{})
	if err != nil {
		t.Fatalf("loading a configuration whose unused profile has a dangling reference: %v", err)
	}

	_, err = config.SelectProfiles(&resolved.Config, []string{"demo"}, "")
	if err == nil {
		t.Fatal("selecting a profile with a dangling value_ref succeeded")
	}
	if !config.ValidationProblems(err) {
		t.Fatalf("error %v does not wrap ErrInvalid", err)
	}
	// The message must name both the variable and the missing key.
	if msg := err.Error(); !strings.Contains(msg, "TOKEN") || !strings.Contains(msg, "nowhere") {
		t.Errorf("message %q names neither the variable nor the missing key", msg)
	}
}

func TestDurationMustBeWholeDays(t *testing.T) {
	msg := loadInvalid(t, `
profiles:
  demo:
    variables:
      - name: TOKEN
        generator: gitlab-pat
        expires_in: 6m
`)
	if !strings.Contains(msg, "whole number of days") {
		t.Errorf("message %q does not explain the <N>d form (FR-013)", msg)
	}
}

func TestRotateBeforeMustBeLessThanExpiresIn(t *testing.T) {
	msg := loadInvalid(t, `
profiles:
  demo:
    variables:
      - name: TOKEN
        generator: gitlab-pat
        expires_in: 30d
        rotate_before: 60d
`)
	if !strings.Contains(msg, "rotate_before") || !strings.Contains(msg, "expires_in") {
		t.Errorf("message %q does not name both durations", msg)
	}
}

func TestUnknownGeneratorKindIsRefused(t *testing.T) {
	msg := loadInvalid(t, `
profiles:
  demo:
    variables:
      - name: TOKEN
        generator: github-app
`)
	if !strings.Contains(msg, "gitlab-pat") {
		t.Errorf("message %q does not name the only supported kind", msg)
	}
}

func TestInstanceNamesAndHostsMustBeUnique(t *testing.T) {
	msg := loadInvalid(t, `
instances:
  - name: forge
    host: git.example.com
    platform: gitlab
    token_env: FORGE_TOKEN
  - name: forge
    host: git.example.com
    platform: gitlab
    token_env: FORGE_TOKEN
`)
	if !strings.Contains(msg, "more than once") {
		t.Errorf("message %q does not report the duplicate instance name", msg)
	}
	if !strings.Contains(msg, "more than one instance") {
		t.Errorf("message %q does not report the duplicate host", msg)
	}
}

func TestInstanceMustNameATokenEnvironmentVariable(t *testing.T) {
	msg := loadInvalid(t, `
instances:
  - name: forge
    host: git.example.com
    platform: gitlab
`)
	if !strings.Contains(msg, "token_env") {
		t.Errorf("message %q does not report the missing token_env", msg)
	}
}

func TestUnknownPlatformIsRefused(t *testing.T) {
	msg := loadInvalid(t, `
instances:
  - name: forge
    host: git.example.com
    platform: gitea
    token_env: FORGE_TOKEN
`)
	if !strings.Contains(msg, "gitea") {
		t.Errorf("message %q does not name the unknown platform", msg)
	}
}

func TestAVariableCarriesNoPlatformOrInstanceField(t *testing.T) {
	// FR-032: one run targets exactly one instance, so a variable must not be
	// able to name a platform or an instance. The schema forbids the fields,
	// and the parser must refuse them rather than ignore them.
	msg := loadInvalid(t, `
values:
  k: v
profiles:
  demo:
    variables:
      - name: TOKEN
        value_ref: k
        platform: gitlab
`)
	if !strings.Contains(msg, "platform") {
		t.Errorf("message %q does not report the forbidden field", msg)
	}
}

func TestEveryProblemIsReportedTogether(t *testing.T) {
	// A configuration with three mistakes should take one edit to fix.
	msg := loadInvalid(t, `
profiles:
  demo:
    variables:
      - name: ONE
      - name: TWO
      - name: THREE
`)
	for _, name := range []string{"ONE", "TWO", "THREE"} {
		if !strings.Contains(msg, name) {
			t.Errorf("message %q does not name %s; failures must be reported together", msg, name)
		}
	}
}
