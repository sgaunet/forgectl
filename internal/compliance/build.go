package compliance

import (
	"fmt"

	"github.com/sgaunet/forgectl/internal/config"
)

// PlanInput is what plan construction needs beyond the report: the local facts
// that decide which branch steps are required.
type PlanInput struct {
	// WantBranch is the configured default branch.
	WantBranch string
	// ActualBranch is what the platform currently serves as default.
	ActualBranch string
	// NewBranchExistsRemotely says whether the configured branch is already on
	// the platform, which decides between the rename-and-push path and the
	// switch-only path (FR-037, FR-038).
	NewBranchExistsRemotely bool
	// NewBranchExistsLocally says whether the working copy already has it.
	NewBranchExistsLocally bool
	// DeleteOldBranch is set only when the maintainer explicitly asked for it
	// (FR-041).
	DeleteOldBranch bool
	// Protection is the configured branch protection.
	Protection config.BranchProtection
	// ForceRotate rotates generated tokens even when no drift was found
	// (FR-053).
	ForceRotate bool
}

// BuildPlan turns an evaluated report into the ordered list of actions apply
// would perform (FR-034).
//
// The order is fixed — branch, then protection including tags, then variables —
// because each step depends on the one before it: protecting a branch that has
// not been pushed yet cannot work, and a protected variable is only exposed on
// refs that are already protected.
//
// A compliant repository yields an empty plan, which is what makes apply
// idempotent: with nothing to do, no confirmation is asked for and no mutating
// call is made (FR-035).
func BuildPlan(report *Report, in PlanInput) Plan {
	var plan Plan

	addBranchActions(&plan, report, in)
	addProtectionActions(&plan, report, in)
	addVariableActions(&plan, report, in)

	return plan
}

// addBranchActions plans the default-branch work, which has four shapes
// (FR-037 through FR-039).
func addBranchActions(plan *Plan, report *Report, in PlanInput) {
	check, found := report.find(CheckBranch)
	if !found || check.Status != StatusFail {
		return
	}

	// A default branch that is neither conventional name is not something apply
	// may touch. The report already carries the manual hint (FR-039).
	if !check.Fixable {
		return
	}

	// The local rename and push happen only when the new branch does not exist
	// on the platform yet. When both names already exist, only the switch and
	// the remote-head update are performed (FR-038).
	if !in.NewBranchExistsRemotely {
		if in.NewBranchExistsLocally {
			plan.Add(Action{
				Kind: ActionPushBranch, Domain: DomainBranch, Destructive: true,
				Target: in.WantBranch,
				Description: fmt.Sprintf("push %s to the remote with upstream tracking",
					in.WantBranch),
			})
		} else {
			plan.Add(Action{
				Kind: ActionRenameBranch, Domain: DomainBranch, Destructive: true,
				Target: in.WantBranch, From: in.ActualBranch,
				Description: fmt.Sprintf("rename the local branch %s to %s",
					in.ActualBranch, in.WantBranch),
			})
			plan.Add(Action{
				Kind: ActionPushBranch, Domain: DomainBranch, Destructive: true,
				Target: in.WantBranch,
				Description: fmt.Sprintf("push %s to the remote with upstream tracking",
					in.WantBranch),
			})
		}
	}

	plan.Add(Action{
		Kind: ActionSetDefaultBranch, Domain: DomainBranch, Destructive: true,
		Target: in.WantBranch, From: in.ActualBranch,
		Description: "set the platform default branch to " + in.WantBranch,
	})

	plan.Add(Action{
		Kind: ActionSetRemoteHead, Domain: DomainBranch,
		Target:      in.WantBranch,
		Description: "point the local remote head at " + in.WantBranch,
	})

	// The old branch goes only when explicitly requested, and only after the
	// new default is in place (FR-041).
	if in.DeleteOldBranch {
		plan.Add(Action{
			Kind: ActionDeleteOldBranch, Domain: DomainBranch, Destructive: true,
			Target:      in.ActualBranch,
			Description: "delete the old remote branch " + in.ActualBranch,
		})
	}
}

// addProtectionActions plans the branch and tag protection work.
//
// Protection is planned not only when its check failed, but also when the check
// was SKIPPED because the target branch does not exist yet while this very plan
// is about to create it. Without that, a repository whose default branch is
// still master would need two applies to become compliant — the first to push
// main, the second to protect it — and SC-004 requires one.
func addProtectionActions(plan *Plan, report *Report, in PlanInput) {
	if protectionNeeded(report, in) {
		plan.Add(Action{
			Kind: ActionSetProtection, Domain: DomainProtection, Destructive: true,
			Target: in.WantBranch,
			Description: fmt.Sprintf("protect branch %s: force-push %s, deletion denied",
				in.WantBranch, permission(in.Protection.AllowForcePush)),
		})
	}

	for _, check := range report.Checks {
		if check.ID != CheckTags || check.Status != StatusFail {
			continue
		}

		plan.Add(Action{
			Kind: ActionProtectTag, Domain: DomainProtection, Destructive: true,
			Target:      check.Pattern,
			Description: "protect the tag pattern " + check.Pattern,
		})
	}
}

// protectionNeeded reports whether the protection step belongs in the plan.
func protectionNeeded(report *Report, in PlanInput) bool {
	check, found := report.find(CheckProtection)
	if !found {
		return false
	}

	switch check.Status {
	case StatusFail:
		return true
	case StatusSkip:
		// The only skip this recovers from is "the branch is not there yet",
		// and only when the branch domain is about to put it there. A skip
		// because protection is disabled in configuration stays a skip, which
		// is why Enabled is consulted here and not on the failure path.
		return in.Protection.Enabled &&
			!in.NewBranchExistsRemotely &&
			branchWillBeCreated(report)
	default:
		return false
	}
}

// branchWillBeCreated reports whether the branch domain is going to create the
// target branch in this same run.
func branchWillBeCreated(report *Report) bool {
	check, found := report.find(CheckBranch)

	return found && check.Status == StatusFail && check.Fixable
}

// addVariableActions plans one write per drifted variable, and one rotation per
// generated variable that needs it.
func addVariableActions(plan *Plan, report *Report, in PlanInput) {
	for _, check := range report.Checks {
		if check.Domain != DomainVars {
			continue
		}

		name := variableName(check.ID)

		if check.Generator != nil {
			// --force-rotate rotates a healthy token too (FR-053).
			if check.Status == StatusFail || in.ForceRotate {
				plan.Add(Action{
					Kind: ActionRotateToken, Domain: DomainVars, Destructive: true,
					Target:      name,
					Description: "rotate the generated token and write it into CI variable " + name,
				})
			}

			continue
		}

		if check.Status == StatusFail {
			plan.Add(Action{
				Kind: ActionSetVariable, Domain: DomainVars, Destructive: true,
				Target:      name,
				Description: "write CI variable " + name,
			})
		}
	}
}

// find returns the first check with the given identifier.
func (r *Report) find(id string) (CheckResult, bool) {
	for _, c := range r.Checks {
		if c.ID == id {
			return c, true
		}
	}

	return CheckResult{}, false
}

// variableName recovers a variable's name from its check identifier.
func variableName(id string) string {
	if len(id) > len(VarPrefix) && id[:len(VarPrefix)] == VarPrefix {
		return id[len(VarPrefix):]
	}

	return id
}

// permission renders a permission flag the way the plan preview reads it.
func permission(allowed bool) string {
	if allowed {
		return "allowed"
	}

	return "denied"
}
