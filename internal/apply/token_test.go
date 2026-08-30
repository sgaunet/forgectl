package apply_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sgaunet/forgectl/internal/apply"
	"github.com/sgaunet/forgectl/internal/compliance"
	"github.com/sgaunet/forgectl/internal/config"
	"github.com/sgaunet/forgectl/internal/forge"
	"github.com/sgaunet/forgectl/internal/forge/forgetest"
)

// clock is the fixed now the expiry arithmetic is measured against.
var clock = time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)

// generated is the go-release shape.
func generated() config.VariableDefinition {
	return config.VariableDefinition{
		Name: "GITLAB_TOKEN", Secret: true, Masked: true, Protected: true,
		Generator: &config.Generator{
			Kind:          config.GeneratorKindGitLabPAT,
			TokenName:     "forgectl",
			Scopes:        []string{"api"},
			Role:          config.AccessMaintainer,
			ExpiresIn:     config.Days(180),
			RotateBefore:  config.Days(60),
			RevokeRotated: true,
		},
	}
}

// rotateAction is the plan step that rotates the token.
func rotateAction() compliance.Action {
	return compliance.Action{
		Kind: compliance.ActionRotateToken, Domain: compliance.DomainVars,
		Target: "GITLAB_TOKEN", Description: "rotate the generated token", Destructive: true,
	}
}

// rotatingExecutor builds an executor that can rotate the generated variable.
func rotatingExecutor(f *forgetest.Fake) *apply.Executor {
	e := executorFor(f)
	e.Now = clock
	e.Definitions = map[string]config.VariableDefinition{"GITLAB_TOKEN": generated()}

	return e
}

func TestRotationCreatesWritesThenRevokes(t *testing.T) {
	// FR-047, FR-048: the ORDER is the whole point. Revoking before the write
	// would risk a CI variable holding a revoked token — a pipeline that
	// authenticates against nothing.
	old := forge.ProjectToken{
		ID: 1, Name: "forgectl", Active: true, ExpiresAt: clock.AddDate(0, 0, 10),
	}

	f := forgetest.New("main").WithTokens(old)
	f.NextTokenID = 2

	result, err := rotatingExecutor(f).Run(context.Background(), planOf(rotateAction()))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Failed() {
		t.Fatalf("rotation failed: %+v", result.Actions)
	}

	want := []string{"CreateProjectToken", "SetVariable", "RevokeProjectToken"}
	if got := f.Mutations(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("call order = %v, want %v", got, want)
	}

	// FR-049: exactly one active token of that name is left.
	if n := f.ActiveTokens("forgectl"); n != 1 {
		t.Errorf("active tokens = %d, want exactly 1", n)
	}

	// The CI variable was written, carrying a value, with the declared
	// attributes.
	if len(f.Writes) != 1 {
		t.Fatalf("writes = %d, want 1", len(f.Writes))
	}
	write := f.Writes[0]
	if write.Name != "GITLAB_TOKEN" || write.ValueLen == 0 {
		t.Errorf("write = %+v, want the variable written with a value", write)
	}
	if !write.Masked || !write.Protected {
		t.Errorf("write = %+v, want the declared attributes carried through", write)
	}
}

func TestTheExpiryIsComputedFromTheConfiguredLifetime(t *testing.T) {
	f := forgetest.New("main")

	if _, err := rotatingExecutor(f).Run(
		context.Background(), planOf(rotateAction())); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(f.Tokens) != 1 {
		t.Fatalf("tokens = %d, want 1 created", len(f.Tokens))
	}

	want := clock.AddDate(0, 0, 180)
	if got := f.Tokens[0].ExpiresAt; !got.Equal(want) {
		t.Errorf("expires_at = %v, want %v (now plus the configured 180d)", got, want)
	}
}

func TestAFailedVariableWriteStrandsTheToken(t *testing.T) {
	// FR-051: the value is disclosed exactly once, so a write that fails after
	// creation cannot be recovered. The report must say so plainly.
	f := forgetest.New("main")
	f.Errors["SetVariable"] = errors.New("the platform rejected the write")

	result, err := rotatingExecutor(f).Run(context.Background(), planOf(rotateAction()))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !result.Failed() {
		t.Fatal("the run reported success though the variable write failed")
	}

	message := result.Actions[0].Error
	for _, want := range []string{"remains active", "rerun"} {
		if !strings.Contains(message, want) {
			t.Errorf("message %q does not say %q", message, want)
		}
	}

	// And nothing was revoked: revocation happens strictly after a successful
	// write, so the old token is still there to fall back on.
	for _, call := range f.Mutations() {
		if call == "RevokeProjectToken" {
			t.Error("a token was revoked after the variable write failed")
		}
	}
}

func TestAnInterruptionLeavesAnExtraTokenRatherThanABrokenVariable(t *testing.T) {
	// The ordering choice, stated as a property: interrupting after creation
	// leaves an EXTRA active token, which the next check reports as ambiguous
	// and the next apply cleans up. The alternative ordering would leave a CI
	// variable holding a revoked token.
	old := forge.ProjectToken{
		ID: 1, Name: "forgectl", Active: true, ExpiresAt: clock.AddDate(0, 0, 10),
	}

	f := forgetest.New("main").WithTokens(old)
	f.NextTokenID = 2
	f.Errors["RevokeProjectToken"] = errors.New("interrupted")

	result, err := rotatingExecutor(f).Run(context.Background(), planOf(rotateAction()))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// A revocation that fails is a warning, not a failure: the variable already
	// holds the new token, so the run achieved what it set out to do.
	if result.Failed() {
		t.Errorf("a failed revocation was treated as a failed run: %+v", result.Actions)
	}
	if n := f.ActiveTokens("forgectl"); n != 2 {
		t.Errorf("active tokens = %d, want 2: the extra one is the tolerable outcome", n)
	}
}

func TestRevocationCanBeDisabled(t *testing.T) {
	// FR-012: revoke_rotated is configurable. Disabling it defers the cleanup
	// to the next rotation, which is a documented consequence.
	old := forge.ProjectToken{
		ID: 1, Name: "forgectl", Active: true, ExpiresAt: clock.AddDate(0, 0, 10),
	}

	f := forgetest.New("main").WithTokens(old)
	f.NextTokenID = 2

	executor := rotatingExecutor(f)
	def := generated()
	def.Generator.RevokeRotated = false
	executor.Definitions["GITLAB_TOKEN"] = def

	if _, err := executor.Run(context.Background(), planOf(rotateAction())); err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, call := range f.Mutations() {
		if call == "RevokeProjectToken" {
			t.Error("a token was revoked despite revoke_rotated being false")
		}
	}
}

func TestRotationOnAPlatformWithoutTokensFails(t *testing.T) {
	// Reaching here on GitHub would be a planning mistake — the check skips the
	// variable — so the executor says so rather than pretending to succeed.
	f := forgetest.New("main")

	executor := rotatingExecutor(f)
	executor.Forge = &writerWithoutTokens{f}

	result, err := executor.Run(context.Background(), planOf(rotateAction()))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Failed() {
		t.Error("rotating on a platform with no project access tokens reported success")
	}
}

// writerWithoutTokens is a forge.Forge that does not issue tokens, standing in
// for GitHub.
type writerWithoutTokens struct{ forge.Forge }
