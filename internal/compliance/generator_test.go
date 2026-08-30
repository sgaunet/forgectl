package compliance_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sgaunet/forgectl/internal/compliance"
	"github.com/sgaunet/forgectl/internal/config"
	"github.com/sgaunet/forgectl/internal/forge"
	"github.com/sgaunet/forgectl/internal/forge/forgetest"
)

// generatedVariable is the go-release shape: a gitlab-pat with the defaults of
// FR-012.
func generatedVariable() config.VariableDefinition {
	return config.VariableDefinition{
		Name:   "GITLAB_TOKEN",
		Secret: true,
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

// clock is the fixed now every generator test measures against.
var clock = time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)

// tokenExpiring builds an active token expiring the given number of days out.
func tokenExpiring(id, days int) forge.ProjectToken {
	return forge.ProjectToken{
		ID: id, Name: "forgectl", Active: true,
		ExpiresAt: clock.AddDate(0, 0, days),
	}
}

func TestNoTokenFailsAsMissing(t *testing.T) {
	// FR-028, US4 acceptance scenario 1.
	f := forgetest.New("main")

	got, err := compliance.EvaluateGenerated(
		context.Background(), f, generatedVariable(), clock)
	if err != nil {
		t.Fatalf("EvaluateGenerated: %v", err)
	}

	if got.Status != compliance.StatusFail {
		t.Fatalf("status = %s, want fail", got.Status)
	}
	if !strings.Contains(got.Actual, "token missing") {
		t.Errorf("actual = %q, want it to report the token as missing", got.Actual)
	}
}

func TestMoreThanOneTokenIsAmbiguous(t *testing.T) {
	// FR-028, US4 acceptance scenario 2: forgectl will not guess which of
	// several tokens is the live one.
	f := forgetest.New("main").WithTokens(tokenExpiring(1, 170), tokenExpiring(2, 120))

	got, err := compliance.EvaluateGenerated(
		context.Background(), f, generatedVariable(), clock)
	if err != nil {
		t.Fatalf("EvaluateGenerated: %v", err)
	}

	if got.Status != compliance.StatusFail {
		t.Fatalf("status = %s, want fail", got.Status)
	}
	if !strings.Contains(got.Actual, "ambiguous") {
		t.Errorf("actual = %q, want it to report ambiguity", got.Actual)
	}
	if !strings.Contains(got.Actual, "rotate") {
		t.Errorf("actual = %q, want it to recommend rotation", got.Actual)
	}
}

func TestATokenAtOrBelowTheThresholdFailsStatingTheDaysRemaining(t *testing.T) {
	// FR-028, US4 acceptance scenario 3.
	tests := []struct {
		name string
		days int
		fail bool
	}{
		{name: "well within its life", days: 170, fail: false},
		{name: "one day above the threshold", days: 61, fail: false},
		{name: "exactly at the threshold", days: 60, fail: true},
		{name: "below the threshold", days: 10, fail: true},
		{name: "already expired", days: -5, fail: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := forgetest.New("main").WithTokens(tokenExpiring(1, tt.days))
			f.Variables["GITLAB_TOKEN"] = forge.VariableState{Exists: true}

			got, err := compliance.EvaluateGenerated(
				context.Background(), f, generatedVariable(), clock)
			if err != nil {
				t.Fatalf("EvaluateGenerated: %v", err)
			}

			failed := got.Status == compliance.StatusFail
			if failed != tt.fail {
				t.Fatalf("status = %s (%s), want fail=%v", got.Status, got.Actual, tt.fail)
			}

			if tt.fail && !strings.Contains(got.Actual, "days") {
				t.Errorf("actual = %q, want it to state the days remaining", got.Actual)
			}

			// FR-055: the entry carries the expiry and the days remaining, pass
			// or fail.
			if got.Generator == nil {
				t.Fatal("the result carries no generator status")
			}
			if got.Generator.Kind != config.GeneratorKindGitLabPAT {
				t.Errorf("kind = %q", got.Generator.Kind)
			}
		})
	}
}

func TestAHealthyTokenWithNoCIVariableFails(t *testing.T) {
	// FR-028: the token exists, but no pipeline can reach it.
	f := forgetest.New("main").WithTokens(tokenExpiring(1, 170))

	got, err := compliance.EvaluateGenerated(
		context.Background(), f, generatedVariable(), clock)
	if err != nil {
		t.Fatalf("EvaluateGenerated: %v", err)
	}

	if got.Status != compliance.StatusFail {
		t.Fatalf("status = %s, want fail", got.Status)
	}
	if got.Actual != "missing" {
		t.Errorf("actual = %q, want missing", got.Actual)
	}
}

func TestAHealthyTokenWithItsVariablePasses(t *testing.T) {
	f := forgetest.New("main").WithTokens(tokenExpiring(1, 170))
	f.Variables["GITLAB_TOKEN"] = forge.VariableState{Exists: true}

	got, err := compliance.EvaluateGenerated(
		context.Background(), f, generatedVariable(), clock)
	if err != nil {
		t.Fatalf("EvaluateGenerated: %v", err)
	}

	if got.Status != compliance.StatusPass {
		t.Fatalf("status = %s (%s), want pass", got.Status, got.Actual)
	}
	if got.Generator.RotateInDays != 110 {
		t.Errorf("rotate_in_days = %d, want 110 (170 remaining less the 60-day threshold)",
			got.Generator.RotateInDays)
	}
	if got.Generator.ExpiresAt.Format(time.DateOnly) != clock.AddDate(0, 0, 170).Format(time.DateOnly) {
		t.Errorf("expires_at = %v", got.Generator.ExpiresAt)
	}
}

func TestAGeneratedVariableOnGitHubSkips(t *testing.T) {
	// FR-029: GitHub has no project access token equivalent, so the variable is
	// skipped and the run does NOT fail.
	f := forgetest.WithoutTokens(forgetest.New("main"))

	got, err := compliance.EvaluateGenerated(
		context.Background(), f, generatedVariable(), clock)
	if err != nil {
		t.Fatalf("EvaluateGenerated: %v", err)
	}

	if got.Status != compliance.StatusSkip {
		t.Fatalf("status = %s, want skip", got.Status)
	}
	if !strings.Contains(got.Reason, "project access token") {
		t.Errorf("reason = %q, want it to explain what the platform lacks", got.Reason)
	}
}

func TestInsufficientRightsSkipsRatherThanCountingAsDrift(t *testing.T) {
	// FR-030: a credential that may not list tokens has not found drift; it has
	// found the limits of its own permissions.
	f := forgetest.New("main")
	f.Errors["ProjectTokens"] = forge.ErrInsufficientRights

	got, err := compliance.EvaluateGenerated(
		context.Background(), f, generatedVariable(), clock)
	if err != nil {
		t.Fatalf("EvaluateGenerated returned an error rather than skipping: %v", err)
	}

	if got.Status != compliance.StatusSkip {
		t.Fatalf("status = %s, want skip", got.Status)
	}
	if !strings.Contains(got.Reason, "may not list") {
		t.Errorf("reason = %q, want it to name the missing right", got.Reason)
	}
}

func TestAPlatformFailureIsStillAFailure(t *testing.T) {
	// Only the two named conditions skip. Anything else is a runtime failure,
	// which exits 1 rather than being quietly swallowed.
	f := forgetest.New("main")
	f.Errors["ProjectTokens"] = errors.New("the platform is unreachable")

	if _, err := compliance.EvaluateGenerated(
		context.Background(), f, generatedVariable(), clock); err == nil {
		t.Fatal("a platform failure was reported as a skip")
	}
}

func TestGeneratorEvaluationIsReadOnly(t *testing.T) {
	f := forgetest.New("main").WithTokens(tokenExpiring(1, 10))

	if _, err := compliance.EvaluateGenerated(
		context.Background(), f, generatedVariable(), clock); err != nil {
		t.Fatalf("EvaluateGenerated: %v", err)
	}

	// Checking a token must never create or revoke one.
	if mutations := f.Mutations(); len(mutations) != 0 {
		t.Errorf("evaluating made mutating calls: %v", mutations)
	}
}
