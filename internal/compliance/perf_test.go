package compliance_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sgaunet/forgectl/internal/compliance"
	"github.com/sgaunet/forgectl/internal/config"
	"github.com/sgaunet/forgectl/internal/forge"
	"github.com/sgaunet/forgectl/internal/forge/forgetest"
)

// budget is the ceiling SC-008 sets: auditing a repository against a profile of
// up to ten variables completes in under ten seconds against a responsive
// platform.
const budget = 10 * time.Second

// latency stands in for a responsive platform. Every check makes at least one
// call, so this is what the budget is actually spent on.
const latency = 20 * time.Millisecond

// slowForge wraps the fake with a fixed per-call delay, so the measurement is
// about the number of calls forgectl makes rather than about the test machine.
type slowForge struct {
	*forgetest.Fake
}

func (s slowForge) DefaultBranch(ctx context.Context) (string, error) {
	time.Sleep(latency)

	return s.Fake.DefaultBranch(ctx)
}

func (s slowForge) BranchExists(ctx context.Context, name string) (bool, error) {
	time.Sleep(latency)

	return s.Fake.BranchExists(ctx, name)
}

func (s slowForge) Protection(ctx context.Context, branch string) (forge.Protection, error) {
	time.Sleep(latency)

	return s.Fake.Protection(ctx, branch)
}

func (s slowForge) TagProtection(ctx context.Context) ([]string, error) {
	time.Sleep(latency)

	return s.Fake.TagProtection(ctx)
}

func (s slowForge) Variable(ctx context.Context, name string, secret bool) (forge.VariableState, error) {
	time.Sleep(latency)

	return s.Fake.Variable(ctx, name, secret)
}

func TestATenVariableProfileAuditsWithinTheBudget(t *testing.T) {
	// SC-008.
	const variables = 10

	fake := forgetest.New("main")
	fake.Protections["main"] = forgetest.Protected(config.AccessMaintainer)
	fake.Tags = []string{"v*"}

	store := map[string]string{}
	selection := config.Selection{
		Names:         []string{"big"},
		ProtectedTags: []string{"v*"},
	}

	for i := range variables {
		name := fmt.Sprintf("VAR_%02d", i)
		key := fmt.Sprintf("key_%02d", i)

		store[key] = "a-value"
		selection.Variables = append(selection.Variables, config.VariableDefinition{
			Name: name, ValueRef: key, Secret: true,
		})

		fake.Variables[name] = forge.VariableState{
			Exists: true, Value: "a-value", ValueReadable: true,
		}
	}

	evaluator := &compliance.Evaluator{
		Forge: slowForge{fake},
		Config: &config.Config{
			Settings: config.Settings{DefaultBranch: "main"},
			Values:   store,
			BranchProtection: config.BranchProtection{
				Enabled: true, PushAccessLevel: config.AccessMaintainer,
			},
		},
		Selection:  selection,
		Instance:   config.Instance{Platform: config.PlatformGitLab},
		Repository: "acme/my-tool",
	}

	start := time.Now()

	report, err := evaluator.Evaluate(context.Background())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	elapsed := time.Since(start)

	if report.Summary.Fail != 0 {
		t.Errorf("the compliant fixture reported %d failures", report.Summary.Fail)
	}
	// branch, protection, one tag, and one per variable.
	if want := 3 + variables; len(report.Checks) != want {
		t.Errorf("checks = %d, want %d", len(report.Checks), want)
	}

	if elapsed > budget {
		t.Errorf("the audit took %v, over the %v budget (SC-008)", elapsed, budget)
	}

	t.Logf("a %d-variable audit took %v against a %v-latency platform, budget %v",
		variables, elapsed, latency, budget)
}
