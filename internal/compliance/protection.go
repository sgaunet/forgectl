package compliance

import (
	"context"
	"fmt"
	"strings"

	"github.com/sgaunet/forgectl/internal/config"
	"github.com/sgaunet/forgectl/internal/forge"
)

// EvaluateProtection verifies that force-push is denied, deletion is denied,
// and — on GitLab, which alone models it — that direct push is restricted to
// the configured access level on the target branch (FR-024).
//
// It skips with a stated reason when the branch does not exist: a branch that
// is not there yet cannot be unprotected, and calling that drift would make a
// fresh repository look broken.
//
// An attribute the platform has no equivalent for is never reported as drift
// (FR-026). That asymmetry is handled here rather than in either client, so the
// two clients stay descriptions of their platform and the judgement lives in
// one place.
func EvaluateProtection(
	ctx context.Context,
	f forge.Reader,
	branch string,
	want config.BranchProtection,
	platform config.Platform,
) (CheckResult, error) {
	if !want.Enabled {
		return Skip(CheckProtection, DomainProtection,
			"branch protection is disabled in the configuration"), nil
	}

	exists, err := f.BranchExists(ctx, branch)
	if err != nil {
		return CheckResult{}, fmt.Errorf("checking whether branch %s exists: %w", branch, err)
	}
	if !exists {
		return Skip(CheckProtection, DomainProtection,
			fmt.Sprintf("branch %s does not exist on the platform yet", branch)), nil
	}

	actual, err := f.Protection(ctx, branch)
	if err != nil {
		return CheckResult{}, fmt.Errorf("reading protection on branch %s: %w", branch, err)
	}

	if !actual.Exists {
		return Fail(CheckProtection, DomainProtection, "protected", "unprotected"), nil
	}

	if drift := protectionDrift(want, actual, platform); len(drift) > 0 {
		return Fail(CheckProtection, DomainProtection,
			describeWanted(want, platform), strings.Join(drift, ", ")), nil
	}

	return Pass(CheckProtection, DomainProtection), nil
}

// protectionDrift lists the attributes that differ, in the platform's own
// terms. An attribute the platform does not model contributes nothing.
func protectionDrift(want config.BranchProtection, actual forge.Protection, platform config.Platform) []string {
	var drift []string

	if actual.AllowForcePush != want.AllowForcePush {
		drift = append(drift, "force-push "+allowed(actual.AllowForcePush))
	}

	// Deleting a protected branch is ALWAYS denied by GitLab, with no toggle
	// (R9). A configured allow_delete: false is therefore satisfied by the
	// branch being protected at all, and must never be reported as drift there.
	if platform == config.PlatformGitHub && actual.AllowDelete != want.AllowDelete {
		drift = append(drift, "deletion "+allowed(actual.AllowDelete))
	}

	// GitHub models no push access level, so it is compared on GitLab only
	// (FR-026).
	if platform == config.PlatformGitLab && actual.PushAccessLevel != want.PushAccessLevel {
		drift = append(drift, fmt.Sprintf("push access %s", actual.PushAccessLevel))
	}

	return drift
}

// describeWanted renders the configured protection in the same terms the drift
// is reported in, so the two lines read as a pair.
func describeWanted(want config.BranchProtection, platform config.Platform) string {
	parts := []string{"force-push " + allowed(want.AllowForcePush)}

	if platform == config.PlatformGitHub {
		parts = append(parts, "deletion "+allowed(want.AllowDelete))
	}
	if platform == config.PlatformGitLab {
		parts = append(parts, fmt.Sprintf("push access %s", want.PushAccessLevel))
	}

	return strings.Join(parts, ", ")
}

// allowed renders a permission flag the way the report reads it.
func allowed(v bool) string {
	if v {
		return "allowed"
	}

	return "denied"
}
