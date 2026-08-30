package compliance

import (
	"context"
	"fmt"

	"github.com/sgaunet/forgectl/internal/forge"
)

// OldConventionalBranch is the name the convention moves away from. Together
// with the configured default branch it forms the pair FR-037 through FR-039
// reason about: a default that is neither is not something apply can fix.
const OldConventionalBranch = "master"

// EvaluateBranch compares the platform's default branch against the configured
// one (FR-023).
//
// A default that is the old conventional name is drift apply can converge. A
// default that is neither conventional name is drift apply must NOT touch: the
// maintainer chose that name deliberately, so the check reports it as not
// auto-fixable and prints a manual hint instead (FR-039).
func EvaluateBranch(ctx context.Context, f forge.Reader, want string) (CheckResult, error) {
	actual, err := f.DefaultBranch(ctx)
	if err != nil {
		return CheckResult{}, fmt.Errorf("reading the default branch: %w", err)
	}

	if actual == want {
		return Pass(CheckBranch, DomainBranch), nil
	}

	result := Fail(CheckBranch, DomainBranch, want, actual)

	if actual != OldConventionalBranch {
		return result.NotFixable(fmt.Sprintf(
			"the default branch is neither %q nor %q; rename it by hand, or set "+
				"settings.default_branch to %q if that is the convention you want",
			OldConventionalBranch, want, actual)), nil
	}

	return result, nil
}
