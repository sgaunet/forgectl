package compliance_test

import (
	"context"
	"testing"

	"github.com/sgaunet/forgectl/internal/compliance"
	"github.com/sgaunet/forgectl/internal/forge/forgetest"
)

func TestEvaluateTagsWithNoPatterns(t *testing.T) {
	// A profile declaring no tag pattern produces no check, and no platform
	// call: the default is none (FR-014).
	f := forgetest.New("main")

	results, err := compliance.EvaluateTags(context.Background(), f, nil)
	if err != nil {
		t.Fatalf("EvaluateTags: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("results = %v, want none", results)
	}
	if calls := f.CallsMade(); len(calls) != 0 {
		t.Errorf("calls = %v, want none", calls)
	}
}

func TestEvaluateTagsReportsEachPatternSeparately(t *testing.T) {
	// One check per pattern, so a report says WHICH pattern is unprotected
	// rather than merely that something is.
	f := forgetest.New("main")
	f.Tags = []string{"v*"}

	results, err := compliance.EvaluateTags(context.Background(), f, []string{"v*", "release-*"})
	if err != nil {
		t.Fatalf("EvaluateTags: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("results = %d, want one per pattern", len(results))
	}

	byPattern := map[string]compliance.CheckResult{}
	for _, r := range results {
		byPattern[r.Pattern] = r
	}

	if got := byPattern["v*"]; got.Status != compliance.StatusPass {
		t.Errorf("v* = %s, want pass", got.Status)
	}
	if got := byPattern["release-*"]; got.Status != compliance.StatusFail {
		t.Errorf("release-* = %s, want fail", got.Status)
	}
}

func TestTagChecksBelongToTheProtectionDomain(t *testing.T) {
	// FR-036: the tags work belongs to the protection domain, so --only
	// protection covers it.
	f := forgetest.New("main")

	results, err := compliance.EvaluateTags(context.Background(), f, []string{"v*"})
	if err != nil {
		t.Fatalf("EvaluateTags: %v", err)
	}

	for _, r := range results {
		if r.Domain != compliance.DomainProtection {
			t.Errorf("domain = %q, want protection", r.Domain)
		}
		if r.ID != compliance.CheckTags {
			t.Errorf("id = %q, want %q", r.ID, compliance.CheckTags)
		}
	}
}

func TestTagComparisonIgnoresTheRefQualification(t *testing.T) {
	// The two platforms spell a protected pattern differently: GitLab stores
	// "v*" while a GitHub ruleset stores "refs/tags/v*". A check about whether
	// the tag is protected must not turn into a check about spelling.
	tests := []struct {
		name     string
		platform []string
		declared string
		wantPass bool
	}{
		{name: "both bare", platform: []string{"v*"}, declared: "v*", wantPass: true},
		{name: "platform qualified", platform: []string{"refs/tags/v*"}, declared: "v*", wantPass: true},
		{name: "declaration qualified", platform: []string{"v*"}, declared: "refs/tags/v*", wantPass: true},
		{name: "genuinely different", platform: []string{"v*"}, declared: "release-*", wantPass: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := forgetest.New("main")
			f.Tags = tt.platform

			results, err := compliance.EvaluateTags(context.Background(), f, []string{tt.declared})
			if err != nil {
				t.Fatalf("EvaluateTags: %v", err)
			}

			passed := results[0].Status == compliance.StatusPass
			if passed != tt.wantPass {
				t.Errorf("%s vs %v: status = %s, want pass=%v",
					tt.declared, tt.platform, results[0].Status, tt.wantPass)
			}
		})
	}
}

func TestEvaluateTagsIsReadOnly(t *testing.T) {
	f := forgetest.New("main")

	if _, err := compliance.EvaluateTags(context.Background(), f, []string{"v*"}); err != nil {
		t.Fatalf("EvaluateTags: %v", err)
	}

	if mutations := f.Mutations(); len(mutations) != 0 {
		t.Errorf("evaluating made mutating calls: %v", mutations)
	}
}
