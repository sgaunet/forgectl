package compliance

import (
	"context"
	"fmt"
	"strings"

	"github.com/sgaunet/forgectl/internal/forge"
)

// EvaluateTags verifies that each tag pattern the selected profiles declare is
// protected (FR-025).
//
// One check is produced per pattern rather than one for all of them, so a
// report says which pattern is unprotected rather than merely that something
// is. Each reports Domain protection: the tags work belongs to the protection
// domain, not to a fourth domain of its own (FR-036).
func EvaluateTags(ctx context.Context, f forge.Reader, patterns []string) ([]CheckResult, error) {
	if len(patterns) == 0 {
		return nil, nil
	}

	protected, err := f.TagProtection(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing protected tags: %w", err)
	}

	results := make([]CheckResult, 0, len(patterns))

	for _, pattern := range patterns {
		result := Pass(CheckTags, DomainProtection)
		if !tagIsProtected(protected, pattern) {
			result = Fail(CheckTags, DomainProtection, "protected", "unprotected")
		}
		result.Pattern = pattern

		results = append(results, result)
	}

	return results, nil
}

// tagIsProtected reports whether a pattern appears in the platform's protected
// list.
//
// The comparison is made on the bare pattern, because the two platforms spell
// it differently: GitLab stores "v*" while a GitHub ruleset stores
// "refs/tags/v*". Normalising here keeps that difference out of the check,
// which should be about whether the tag is protected, not about spelling.
func tagIsProtected(protected []string, pattern string) bool {
	want := bareTag(pattern)

	for _, candidate := range protected {
		if bareTag(candidate) == want {
			return true
		}
	}

	return false
}

// bareTag strips the refs/tags/ qualification a ruleset condition carries.
func bareTag(pattern string) string {
	return strings.TrimPrefix(pattern, "refs/tags/")
}
