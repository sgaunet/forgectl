package apply_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sgaunet/forgectl/internal/apply"
	"github.com/sgaunet/forgectl/internal/compliance"
	"github.com/sgaunet/forgectl/internal/config"
	"github.com/sgaunet/forgectl/internal/forge/forgetest"
)

// planOf builds a plan from the given actions.
func planOf(actions ...compliance.Action) compliance.Plan {
	return compliance.Plan{Actions: actions}
}

// protectionAction is the branch-protection step.
func protectionAction() compliance.Action {
	return compliance.Action{
		Kind: compliance.ActionSetProtection, Domain: compliance.DomainProtection,
		Target: "main", Description: "protect branch main", Destructive: true,
	}
}

// tagAction is the tag-protection step for one pattern.
func tagAction(pattern string) compliance.Action {
	return compliance.Action{
		Kind: compliance.ActionProtectTag, Domain: compliance.DomainProtection,
		Target: pattern, Description: "protect the tag pattern " + pattern, Destructive: true,
	}
}

// executorFor builds an executor over the fake forge.
func executorFor(f *forgetest.Fake) *apply.Executor {
	return &apply.Executor{
		Forge: f,
		Config: &config.Config{
			Settings: config.Settings{DefaultBranch: "main"},
			BranchProtection: config.BranchProtection{
				Enabled: true, PushAccessLevel: config.AccessMaintainer,
			},
		},
	}
}

// statuses lists the outcome of each executed action, in order.
func statuses(result apply.Result) []string {
	out := make([]string, 0, len(result.Actions))
	for _, a := range result.Actions {
		out = append(out, a.Status.String())
	}

	return out
}

func TestAnEmptyPlanMakesNoMutatingCall(t *testing.T) {
	// FR-035, SC-002: the second apply of a converged repository changes
	// nothing and calls nothing.
	f := forgetest.New("main")

	result, err := executorFor(f).Run(context.Background(), compliance.Plan{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.Actions) != 0 {
		t.Errorf("actions = %v, want none", statuses(result))
	}
	if mutations := f.Mutations(); len(mutations) != 0 {
		t.Errorf("an empty plan made mutating calls: %v", mutations)
	}
}

func TestActionsRunInPlanOrder(t *testing.T) {
	f := forgetest.New("main")

	plan := planOf(protectionAction(), tagAction("v*"), tagAction("release-*"))

	result, err := executorFor(f).Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, status := range statuses(result) {
		if status != "done" {
			t.Fatalf("statuses = %v, want all done", statuses(result))
		}
	}

	want := []string{"SetProtection", "ProtectTag", "ProtectTag"}
	if got := f.Mutations(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("calls = %v, want %v", got, want)
	}
}

func TestPartialFailureReportsWhatSucceededAndWhatDidNot(t *testing.T) {
	// FR-045: on partial failure apply reports which actions succeeded and
	// which did not, and a rerun converges from there.
	f := forgetest.New("main")
	f.Errors["ProtectTag"] = errors.New("the platform rejected the pattern")

	plan := planOf(protectionAction(), tagAction("v*"), tagAction("release-*"))

	result, err := executorFor(f).Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := []string{"done", "failed", "skipped"}
	if got := statuses(result); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("statuses = %v, want %v", got, want)
	}

	if !result.Failed() {
		t.Error("Failed() = false after an action failed")
	}

	// The failure carries its reason, and the untried action says why it was
	// not attempted.
	if result.Actions[1].Error == "" {
		t.Error("the failed action carries no message")
	}
	if !strings.Contains(result.Actions[2].Error, "not attempted") {
		t.Errorf("the skipped action says %q, want it to say it was not attempted",
			result.Actions[2].Error)
	}
}

func TestExecutionStopsAtTheFirstFailure(t *testing.T) {
	// The order of FR-034 exists because each step depends on the one before
	// it, so pressing on past a failure would attempt work whose precondition
	// is known to be missing.
	f := forgetest.New("main")
	f.Errors["SetProtection"] = errors.New("no")

	plan := planOf(protectionAction(), tagAction("v*"))

	if _, err := executorFor(f).Run(context.Background(), plan); err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, call := range f.Mutations() {
		if call == "ProtectTag" {
			t.Error("execution continued past a failed action")
		}
	}
}

func TestInterruptionStopsTheRun(t *testing.T) {
	// CLI-005: an interrupted apply stops at the current step and reports what
	// completed; rerunning converges.
	f := forgetest.New("main")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	plan := planOf(protectionAction(), tagAction("v*"))

	result, err := executorFor(f).Run(ctx, plan)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if mutations := f.Mutations(); len(mutations) != 0 {
		t.Errorf("a cancelled run made mutating calls: %v", mutations)
	}
	for _, action := range result.Actions {
		if action.Status != compliance.ActionSkipped {
			t.Errorf("action %s = %s, want skipped", action.Kind, action.Status)
		}
		if !strings.Contains(action.Error, "interrupted") {
			t.Errorf("action %s says %q, want it to say the run was interrupted",
				action.Kind, action.Error)
		}
	}
}

func TestProtectionUsesTheConfiguredSettings(t *testing.T) {
	f := forgetest.New("main")

	executor := executorFor(f)
	executor.Config.BranchProtection.AllowForcePush = false
	executor.Config.BranchProtection.PushAccessLevel = config.AccessDeveloper

	if _, err := executor.Run(context.Background(), planOf(protectionAction())); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := f.Protections["main"]
	if !got.Exists {
		t.Fatal("the branch was not protected")
	}
	if got.AllowForcePush {
		t.Error("force-push was left allowed despite the configuration denying it")
	}
	if got.PushAccessLevel != config.AccessDeveloper {
		t.Errorf("push access level = %q, want the configured developer", got.PushAccessLevel)
	}
}

func TestAnUnknownActionIsReportedRatherThanIgnored(t *testing.T) {
	// A plan carrying an action no handler covers must fail loudly: silently
	// skipping it would report success on a repository that never converged.
	f := forgetest.New("main")

	plan := planOf(compliance.Action{
		Kind: "invent-a-branch", Domain: compliance.DomainBranch, Description: "?",
	})

	result, err := executorFor(f).Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !result.Failed() {
		t.Error("an action with no handler was reported as done")
	}
}
