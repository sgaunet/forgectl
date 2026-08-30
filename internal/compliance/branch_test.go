package compliance_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sgaunet/forgectl/internal/compliance"
	"github.com/sgaunet/forgectl/internal/forge/forgetest"
)

func TestEvaluateBranch(t *testing.T) {
	tests := []struct {
		name        string
		platformHas string
		want        string
		status      compliance.CheckStatus
		fixable     bool
		expected    string
		actual      string
	}{
		{
			name:        "the platform default is the configured one",
			platformHas: "main",
			want:        "main",
			status:      compliance.StatusPass,
			fixable:     true,
		},
		{
			name:        "the platform default is the old conventional name",
			platformHas: "master",
			want:        "main",
			status:      compliance.StatusFail,
			fixable:     true,
			expected:    "main",
			actual:      "master",
		},
		{
			name:        "the platform default is neither conventional name",
			platformHas: "trunk",
			want:        "main",
			status:      compliance.StatusFail,
			fixable:     false,
			expected:    "main",
			actual:      "trunk",
		},
		{
			name:        "a configuration whose convention is master is satisfied by master",
			platformHas: "master",
			want:        "master",
			status:      compliance.StatusPass,
			fixable:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := forgetest.New(tt.platformHas)

			got, err := compliance.EvaluateBranch(context.Background(), f, tt.want)
			if err != nil {
				t.Fatalf("EvaluateBranch: %v", err)
			}

			if got.Status != tt.status {
				t.Errorf("status = %s, want %s", got.Status, tt.status)
			}
			if got.ID != compliance.CheckBranch {
				t.Errorf("id = %q, want %q", got.ID, compliance.CheckBranch)
			}
			if got.Domain != compliance.DomainBranch {
				t.Errorf("domain = %q, want branch", got.Domain)
			}

			// FR-022: a failure carries the expected and actual state.
			if got.Expected != tt.expected {
				t.Errorf("expected = %q, want %q", got.Expected, tt.expected)
			}
			if got.Actual != tt.actual {
				t.Errorf("actual = %q, want %q", got.Actual, tt.actual)
			}

			// FR-039: a default that is neither conventional name is not fixable.
			if got.Status == compliance.StatusFail && got.Fixable != tt.fixable {
				t.Errorf("fixable = %v, want %v", got.Fixable, tt.fixable)
			}

			// The read-only guarantee: evaluating changed nothing (FR-031).
			if mutations := f.Mutations(); len(mutations) != 0 {
				t.Errorf("evaluating made mutating calls: %v", mutations)
			}
		})
	}
}

func TestEvaluateBranchPrintsAManualHintWhenNotFixable(t *testing.T) {
	f := forgetest.New("trunk")

	got, err := compliance.EvaluateBranch(context.Background(), f, "main")
	if err != nil {
		t.Fatalf("EvaluateBranch: %v", err)
	}

	// FR-039: the hint must be actionable, naming both conventional names and
	// the setting that would accept the current one.
	for _, want := range []string{"master", "main", "trunk", "settings.default_branch"} {
		if !strings.Contains(got.Reason, want) {
			t.Errorf("hint %q does not mention %q", got.Reason, want)
		}
	}
}

func TestEvaluateBranchPropagatesAPlatformFailure(t *testing.T) {
	f := forgetest.New("main")
	f.Errors["DefaultBranch"] = errors.New("the platform is unreachable")

	// A platform failure is a runtime failure, not drift: it must reach the
	// caller as an error so the run exits 1 rather than 3 (CLI-002).
	if _, err := compliance.EvaluateBranch(context.Background(), f, "main"); err == nil {
		t.Fatal("EvaluateBranch swallowed a platform failure")
	}
}
