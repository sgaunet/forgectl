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

// wanted is the default protection of FR-015: enabled, force-push denied,
// deletion denied, direct push restricted to maintainers.
func wanted() config.BranchProtection {
	return config.BranchProtection{
		Enabled:         true,
		AllowForcePush:  false,
		AllowDelete:     false,
		PushAccessLevel: config.AccessMaintainer,
	}
}

func TestEvaluateProtectionSkipsWhenTheBranchDoesNotExist(t *testing.T) {
	// US1 acceptance scenario 3: the skip is stated with its reason, and is
	// counted separately from a failure.
	f := forgetest.New("master")

	got, err := compliance.EvaluateProtection(
		context.Background(), f, "main", wanted(), config.PlatformGitLab)
	if err != nil {
		t.Fatalf("EvaluateProtection: %v", err)
	}

	if got.Status != compliance.StatusSkip {
		t.Fatalf("status = %s, want skip", got.Status)
	}
	if !strings.Contains(got.Reason, "main") {
		t.Errorf("reason %q does not name the branch", got.Reason)
	}
	if got.Domain != compliance.DomainProtection {
		t.Errorf("domain = %q, want protection", got.Domain)
	}
}

func TestEvaluateProtectionFailsOnAnUnprotectedBranch(t *testing.T) {
	f := forgetest.New("main")

	got, err := compliance.EvaluateProtection(
		context.Background(), f, "main", wanted(), config.PlatformGitLab)
	if err != nil {
		t.Fatalf("EvaluateProtection: %v", err)
	}

	if got.Status != compliance.StatusFail {
		t.Fatalf("status = %s, want fail", got.Status)
	}
	if got.Actual != "unprotected" {
		t.Errorf("actual = %q, want unprotected", got.Actual)
	}
}

func TestEvaluateProtectionPassesOnACompliantBranch(t *testing.T) {
	for _, platform := range []config.Platform{config.PlatformGitLab, config.PlatformGitHub} {
		t.Run(platform.String(), func(t *testing.T) {
			f := forgetest.New("main")
			f.Protections["main"] = forgetest.Protected(config.AccessMaintainer)

			got, err := compliance.EvaluateProtection(
				context.Background(), f, "main", wanted(), platform)
			if err != nil {
				t.Fatalf("EvaluateProtection: %v", err)
			}
			if got.Status != compliance.StatusPass {
				t.Errorf("status = %s (%s / %s), want pass", got.Status, got.Expected, got.Actual)
			}
		})
	}
}

func TestEvaluateProtectionReportsForcePushDrift(t *testing.T) {
	f := forgetest.New("main")
	f.Protections["main"] = forge.Protection{
		Exists:          true,
		AllowForcePush:  true,
		PushAccessLevel: config.AccessMaintainer,
	}

	got, err := compliance.EvaluateProtection(
		context.Background(), f, "main", wanted(), config.PlatformGitLab)
	if err != nil {
		t.Fatalf("EvaluateProtection: %v", err)
	}

	if got.Status != compliance.StatusFail {
		t.Fatalf("status = %s, want fail", got.Status)
	}
	if !strings.Contains(got.Actual, "force-push allowed") {
		t.Errorf("actual = %q, want it to report force-push as allowed", got.Actual)
	}
}

func TestDeletionIsNeverDriftOnGitLab(t *testing.T) {
	// R9: deleting a protected branch is always denied by GitLab, with no
	// toggle, so allow_delete is satisfied by the branch being protected at all.
	// The client reports AllowDelete false; the check must not compare it.
	f := forgetest.New("main")
	f.Protections["main"] = forge.Protection{
		Exists:          true,
		AllowDelete:     true, // a client bug, or a platform that grew the toggle
		PushAccessLevel: config.AccessMaintainer,
	}

	got, err := compliance.EvaluateProtection(
		context.Background(), f, "main", wanted(), config.PlatformGitLab)
	if err != nil {
		t.Fatalf("EvaluateProtection: %v", err)
	}
	if got.Status != compliance.StatusPass {
		t.Errorf("status = %s (%s), want pass: deletion is inherent on GitLab", got.Status, got.Actual)
	}

	// On GitHub, where the deletion rule is explicit, the same state IS drift.
	got, err = compliance.EvaluateProtection(
		context.Background(), f, "main", wanted(), config.PlatformGitHub)
	if err != nil {
		t.Fatalf("EvaluateProtection: %v", err)
	}
	if got.Status != compliance.StatusFail {
		t.Errorf("status = %s, want fail: deletion is explicit on GitHub", got.Status)
	}
}

func TestPushAccessLevelIsComparedOnGitLabOnly(t *testing.T) {
	// FR-026: GitHub models no push access level, so its zero value must not be
	// reported as drift.
	f := forgetest.New("main")
	f.Protections["main"] = forge.Protection{
		Exists: true,
		// GitHub's client leaves this at the zero value.
		PushAccessLevel: "",
	}

	got, err := compliance.EvaluateProtection(
		context.Background(), f, "main", wanted(), config.PlatformGitHub)
	if err != nil {
		t.Fatalf("EvaluateProtection: %v", err)
	}
	if got.Status != compliance.StatusPass {
		t.Errorf("status = %s (%s), want pass: GitHub models no push access level",
			got.Status, got.Actual)
	}

	// The same state on GitLab is genuine drift.
	got, err = compliance.EvaluateProtection(
		context.Background(), f, "main", wanted(), config.PlatformGitLab)
	if err != nil {
		t.Fatalf("EvaluateProtection: %v", err)
	}
	if got.Status != compliance.StatusFail {
		t.Errorf("status = %s, want fail on GitLab", got.Status)
	}
}

func TestEvaluateProtectionSkipsWhenDisabled(t *testing.T) {
	f := forgetest.New("main")

	want := wanted()
	want.Enabled = false

	got, err := compliance.EvaluateProtection(
		context.Background(), f, "main", want, config.PlatformGitLab)
	if err != nil {
		t.Fatalf("EvaluateProtection: %v", err)
	}
	if got.Status != compliance.StatusSkip {
		t.Errorf("status = %s, want skip when protection is disabled", got.Status)
	}
}

func TestEvaluateProtectionIsReadOnly(t *testing.T) {
	f := forgetest.New("main")
	f.Protections["main"] = forgetest.Protected(config.AccessDeveloper)

	if _, err := compliance.EvaluateProtection(
		context.Background(), f, "main", wanted(), config.PlatformGitLab); err != nil {
		t.Fatalf("EvaluateProtection: %v", err)
	}

	if mutations := f.Mutations(); len(mutations) != 0 {
		t.Errorf("evaluating made mutating calls: %v", mutations)
	}
}
