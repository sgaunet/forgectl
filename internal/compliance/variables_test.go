package compliance_test

import (
	"context"
	"strings"
	"testing"

	"github.com/sgaunet/forgectl/internal/compliance"
	"github.com/sgaunet/forgectl/internal/config"
	"github.com/sgaunet/forgectl/internal/forge"
	"github.com/sgaunet/forgectl/internal/forge/forgetest"
)

// refVariable is a variable whose value comes from the shared store.
func refVariable(name string) config.VariableDefinition {
	return config.VariableDefinition{Name: name, ValueRef: "shared", Secret: true}
}

func TestAMissingVariableFails(t *testing.T) {
	f := forgetest.New("main")

	got, err := compliance.EvaluateVariable(context.Background(), f,
		refVariable("GALAXY_API_TOKEN"), compliance.Expected{Value: "v", Known: true}, config.PlatformGitLab)
	if err != nil {
		t.Fatalf("EvaluateVariable: %v", err)
	}

	if got.Status != compliance.StatusFail {
		t.Fatalf("status = %s, want fail", got.Status)
	}
	if got.Actual != "missing" {
		t.Errorf("actual = %q, want missing", got.Actual)
	}
	if got.ID != "vars:GALAXY_API_TOKEN" {
		t.Errorf("id = %q, want vars:GALAXY_API_TOKEN (FR-021)", got.ID)
	}
	if got.Domain != compliance.DomainVars {
		t.Errorf("domain = %q, want vars", got.Domain)
	}
}

func TestAGitLabValueThatDiffersIsDrift(t *testing.T) {
	// FR-027: GitLab discloses the value, so a difference is real drift.
	f := forgetest.New("main")
	f.Variables["TOKEN"] = forge.VariableState{
		Exists: true, Masked: false, Value: "on-the-platform", ValueReadable: true,
	}

	got, err := compliance.EvaluateVariable(context.Background(), f,
		refVariable("TOKEN"), compliance.Expected{Value: "configured", Known: true}, config.PlatformGitLab)
	if err != nil {
		t.Fatalf("EvaluateVariable: %v", err)
	}

	if got.Status != compliance.StatusFail {
		t.Fatalf("status = %s, want fail", got.Status)
	}
	// FR-054: the report says "differs", never the differing value.
	if !strings.Contains(got.Actual, "differs") {
		t.Errorf("actual = %q, want it to report differs", got.Actual)
	}
	for _, sentinel := range []string{"on-the-platform", "configured"} {
		if strings.Contains(got.Actual+got.Expected+got.Reason, sentinel) {
			t.Errorf("the result carries a value: %+v", got)
		}
	}
}

func TestAGitHubCredentialIsNeverComparedByValue(t *testing.T) {
	// FR-027: GitHub's Actions credentials are write-only, so their values are
	// not compared and never reported as drift.
	f := forgetest.New("main")
	f.Variables["TOKEN"] = forge.VariableState{
		Exists: true, Value: "", ValueReadable: false,
	}

	got, err := compliance.EvaluateVariable(context.Background(), f,
		refVariable("TOKEN"), compliance.Expected{Value: "configured", Known: true}, config.PlatformGitHub)
	if err != nil {
		t.Fatalf("EvaluateVariable: %v", err)
	}

	if got.Status != compliance.StatusPass {
		t.Errorf("status = %s (%s), want pass: a write-only value cannot drift",
			got.Status, got.Actual)
	}
}

func TestMaskedAndProtectedAreComparedOnGitLabOnly(t *testing.T) {
	// FR-026: an attribute the platform has no equivalent for is never drift.
	def := config.VariableDefinition{
		Name: "TOKEN", ValueRef: "shared", Secret: true, Masked: true, Protected: true,
	}
	expected := compliance.Expected{Value: "configured", Known: true}

	// The platform reports neither attribute — GitHub's zero values.
	f := forgetest.New("main")
	f.Variables["TOKEN"] = forge.VariableState{Exists: true, ValueReadable: false}

	got, err := compliance.EvaluateVariable(
		context.Background(), f, def, expected, config.PlatformGitHub)
	if err != nil {
		t.Fatalf("EvaluateVariable: %v", err)
	}
	if got.Status != compliance.StatusPass {
		t.Errorf("status = %s (%s), want pass: GitHub models neither attribute",
			got.Status, got.Actual)
	}

	// The same state on GitLab, which does model them, is drift.
	f2 := forgetest.New("main")
	f2.Variables["TOKEN"] = forge.VariableState{
		Exists: true, Value: "configured", ValueReadable: true,
	}

	got, err = compliance.EvaluateVariable(
		context.Background(), f2, def, expected, config.PlatformGitLab)
	if err != nil {
		t.Fatalf("EvaluateVariable: %v", err)
	}
	if got.Status != compliance.StatusFail {
		t.Fatalf("status = %s, want fail on GitLab", got.Status)
	}
	if !strings.Contains(got.Actual, "masked no") || !strings.Contains(got.Actual, "protected no") {
		t.Errorf("actual = %q, want it to name both attributes", got.Actual)
	}
}

func TestACompliantVariablePasses(t *testing.T) {
	f := forgetest.New("main")
	f.Variables["TOKEN"] = forge.VariableState{
		Exists: true, Masked: true, Protected: false,
		Value: "configured", ValueReadable: true,
	}

	def := config.VariableDefinition{Name: "TOKEN", ValueRef: "shared", Secret: true, Masked: true}

	got, err := compliance.EvaluateVariable(context.Background(), f, def,
		compliance.Expected{Value: "configured", Known: true}, config.PlatformGitLab)
	if err != nil {
		t.Fatalf("EvaluateVariable: %v", err)
	}
	if got.Status != compliance.StatusPass {
		t.Errorf("status = %s (%s), want pass", got.Status, got.Actual)
	}
}

func TestAGeneratedVariableIsNeverComparedByValue(t *testing.T) {
	// Its value comes from the platform, so there is nothing to compare it to.
	f := forgetest.New("main")
	f.Variables["GITLAB_TOKEN"] = forge.VariableState{
		Exists: true, Value: "whatever-the-platform-holds", ValueReadable: true,
	}

	def := config.VariableDefinition{
		Name:      "GITLAB_TOKEN",
		Generator: &config.Generator{Kind: config.GeneratorKindGitLabPAT},
		Secret:    true,
	}

	// A generated variable's value comes from the platform, so there is nothing
	// to compare it against.
	got, err := compliance.EvaluateVariable(
		context.Background(), f, def, compliance.Expected{}, config.PlatformGitLab)
	if err != nil {
		t.Fatalf("EvaluateVariable: %v", err)
	}
	if got.Status != compliance.StatusPass {
		t.Errorf("status = %s (%s), want pass", got.Status, got.Actual)
	}
}

func TestVariableEvaluationIsReadOnly(t *testing.T) {
	f := forgetest.New("main")

	if _, err := compliance.EvaluateVariable(context.Background(), f,
		refVariable("TOKEN"), compliance.Expected{Value: "v", Known: true},
		config.PlatformGitLab); err != nil {
		t.Fatalf("EvaluateVariable: %v", err)
	}

	if mutations := f.Mutations(); len(mutations) != 0 {
		t.Errorf("evaluating made mutating calls: %v", mutations)
	}
}
