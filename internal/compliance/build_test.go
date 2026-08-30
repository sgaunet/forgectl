package compliance_test

import (
	"strings"
	"testing"

	"github.com/sgaunet/forgectl/internal/compliance"
	"github.com/sgaunet/forgectl/internal/config"
)

// driftedBranchReport is a report whose only failure is the branch check.
func driftedBranchReport(fixable bool) *compliance.Report {
	check := compliance.Fail(compliance.CheckBranch, compliance.DomainBranch, "main", "master")
	if !fixable {
		check = compliance.Fail(compliance.CheckBranch, compliance.DomainBranch, "main", "trunk").
			NotFixable("rename it by hand")
	}

	r := &compliance.Report{Checks: []compliance.CheckResult{check}}
	r.Summarise()

	return r
}

// kinds lists the action kinds of a plan, in order.
func kinds(plan compliance.Plan) []string {
	out := make([]string, 0, len(plan.Actions))
	for _, a := range plan.Actions {
		out = append(out, a.Kind.String())
	}

	return out
}

func TestBranchStateTransitions(t *testing.T) {
	// The four shapes of FR-037 through FR-039.
	tests := []struct {
		name      string
		fixable   bool
		remotely  bool
		locally   bool
		deleteOld bool
		want      []string
	}{
		{
			name:    "master with no main: rename, push, switch, set head",
			fixable: true,
			want: []string{
				"rename-branch", "push-branch", "set-default-branch", "set-remote-head",
			},
		},
		{
			name:    "master with main absent locally but present remotely: switch only",
			fixable: true, remotely: true,
			want: []string{"set-default-branch", "set-remote-head"},
		},
		{
			name:    "main exists locally but not remotely: push, switch, set head",
			fixable: true, locally: true,
			want: []string{"push-branch", "set-default-branch", "set-remote-head"},
		},
		{
			name:      "the old branch is deleted only when asked, and last",
			fixable:   true,
			remotely:  true,
			deleteOld: true,
			want:      []string{"set-default-branch", "set-remote-head", "delete-old-branch"},
		},
		{
			name:    "a default that is neither conventional name plans nothing",
			fixable: false,
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := compliance.BuildPlan(driftedBranchReport(tt.fixable), compliance.PlanInput{
				WantBranch:              "main",
				ActualBranch:            "master",
				NewBranchExistsRemotely: tt.remotely,
				NewBranchExistsLocally:  tt.locally,
				DeleteOldBranch:         tt.deleteOld,
			})

			got := kinds(plan)
			if len(got) != len(tt.want) {
				t.Fatalf("plan = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("plan = %v, want %v", got, tt.want)

					break
				}
			}
		})
	}
}

func TestACompliantReportYieldsAnEmptyPlan(t *testing.T) {
	// FR-035, SC-002: the second apply must have nothing to do.
	r := &compliance.Report{
		Checks: []compliance.CheckResult{
			compliance.Pass(compliance.CheckBranch, compliance.DomainBranch),
			compliance.Pass(compliance.CheckProtection, compliance.DomainProtection),
		},
	}
	r.Summarise()

	plan := compliance.BuildPlan(r, compliance.PlanInput{WantBranch: "main", ActualBranch: "main"})

	if !plan.Empty() {
		t.Errorf("plan = %v, want empty", kinds(plan))
	}
}

func TestASkipPlansNothing(t *testing.T) {
	// A check that could not be evaluated is not drift, so it plans no work.
	r := &compliance.Report{
		Checks: []compliance.CheckResult{
			compliance.Skip(compliance.CheckProtection, compliance.DomainProtection,
				"branch main does not exist yet"),
		},
	}
	r.Summarise()

	plan := compliance.BuildPlan(r, compliance.PlanInput{WantBranch: "main", ActualBranch: "main"})

	if !plan.Empty() {
		t.Errorf("plan = %v, want empty", kinds(plan))
	}
}

func TestPlanOrderIsBranchThenProtectionThenVars(t *testing.T) {
	// FR-034: the order is fixed, because each step depends on the one before.
	tags := compliance.Fail(compliance.CheckTags, compliance.DomainProtection, "protected", "unprotected")
	tags.Pattern = "v*"

	r := &compliance.Report{
		Checks: []compliance.CheckResult{
			// Deliberately out of order, to prove the plan does not simply
			// follow the report.
			compliance.Fail(compliance.VarCheckID("TOKEN"), compliance.DomainVars, "present", "missing"),
			tags,
			compliance.Fail(compliance.CheckProtection, compliance.DomainProtection,
				"protected", "unprotected"),
			compliance.Fail(compliance.CheckBranch, compliance.DomainBranch, "main", "master"),
		},
	}
	r.Summarise()

	plan := compliance.BuildPlan(r, compliance.PlanInput{
		WantBranch: "main", ActualBranch: "master", NewBranchExistsRemotely: true,
	})

	want := []string{"set-default-branch", "set-remote-head", "set-protection", "protect-tag", "set-variable"}
	got := kinds(plan)

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("plan order = %v, want %v", got, want)
	}
}

func TestPlanFilteringByDomain(t *testing.T) {
	// FR-036: --only and --skip select domains, and the tags work belongs to
	// the protection domain rather than to one of its own.
	tags := compliance.Fail(compliance.CheckTags, compliance.DomainProtection, "protected", "unprotected")
	tags.Pattern = "v*"

	r := &compliance.Report{
		Checks: []compliance.CheckResult{
			compliance.Fail(compliance.CheckBranch, compliance.DomainBranch, "main", "master"),
			compliance.Fail(compliance.CheckProtection, compliance.DomainProtection,
				"protected", "unprotected"),
			tags,
			compliance.Fail(compliance.VarCheckID("TOKEN"), compliance.DomainVars, "present", "missing"),
		},
	}
	r.Summarise()

	plan := compliance.BuildPlan(r, compliance.PlanInput{
		WantBranch: "main", ActualBranch: "master", NewBranchExistsRemotely: true,
	})

	tests := []struct {
		name string
		only []compliance.Domain
		skip []compliance.Domain
		want []string
	}{
		{
			name: "no restriction keeps everything",
			want: []string{"set-default-branch", "set-remote-head", "set-protection", "protect-tag", "set-variable"},
		},
		{
			name: "--only protection keeps the tags work too",
			only: []compliance.Domain{compliance.DomainProtection},
			want: []string{"set-protection", "protect-tag"},
		},
		{
			name: "--skip branch drops the branch work",
			skip: []compliance.Domain{compliance.DomainBranch},
			want: []string{"set-protection", "protect-tag", "set-variable"},
		},
		{
			name: "--only branch,vars",
			only: []compliance.Domain{compliance.DomainBranch, compliance.DomainVars},
			want: []string{"set-default-branch", "set-remote-head", "set-variable"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := kinds(plan.Filter(tt.only, tt.skip))
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("plan = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlanDescriptionsCarryNoValue(t *testing.T) {
	// FR-054: the plan preview is shown to the maintainer, so it must describe
	// state and never a value.
	tags := compliance.Fail(compliance.CheckTags, compliance.DomainProtection, "protected", "unprotected")
	tags.Pattern = "v*"

	r := &compliance.Report{
		Checks: []compliance.CheckResult{
			compliance.Fail(compliance.CheckBranch, compliance.DomainBranch, "main", "master"),
			compliance.Fail(compliance.CheckProtection, compliance.DomainProtection,
				"protected", "unprotected"),
			tags,
			compliance.Fail(compliance.VarCheckID("GALAXY_API_TOKEN"), compliance.DomainVars,
				"present", "missing"),
		},
	}
	r.Summarise()

	plan := compliance.BuildPlan(r, compliance.PlanInput{
		WantBranch: "main", ActualBranch: "master",
		Protection: config.BranchProtection{PushAccessLevel: config.AccessMaintainer},
	})

	for _, action := range plan.Actions {
		if action.Description == "" {
			t.Errorf("action %s has no description for the plan preview", action.Kind)
		}
		for _, sentinel := range []string{"glpat-", "ssh-rsa", "s3cr3t"} {
			if strings.Contains(action.Description, sentinel) {
				t.Errorf("action description %q carries a value", action.Description)
			}
		}
	}
}

func TestEveryPlannedChangeIsMarkedDestructive(t *testing.T) {
	// CLI-003: each of these actions needs --yes or an interactive
	// confirmation, so each must be marked.
	tags := compliance.Fail(compliance.CheckTags, compliance.DomainProtection, "protected", "unprotected")
	tags.Pattern = "v*"

	r := &compliance.Report{
		Checks: []compliance.CheckResult{
			compliance.Fail(compliance.CheckBranch, compliance.DomainBranch, "main", "master"),
			compliance.Fail(compliance.CheckProtection, compliance.DomainProtection,
				"protected", "unprotected"),
			tags,
		},
	}
	r.Summarise()

	plan := compliance.BuildPlan(r, compliance.PlanInput{
		WantBranch: "main", ActualBranch: "master", DeleteOldBranch: true,
	})

	if !plan.Destructive() {
		t.Fatal("a plan that renames, pushes, switches, protects and deletes is not marked destructive")
	}

	mustBeDestructive := map[compliance.ActionKind]bool{
		compliance.ActionRenameBranch: true, compliance.ActionPushBranch: true,
		compliance.ActionSetDefaultBranch: true, compliance.ActionDeleteOldBranch: true,
		compliance.ActionSetProtection: true, compliance.ActionProtectTag: true,
	}

	for _, action := range plan.Actions {
		if mustBeDestructive[action.Kind] && !action.Destructive {
			t.Errorf("action %s is not marked destructive", action.Kind)
		}
	}
}
