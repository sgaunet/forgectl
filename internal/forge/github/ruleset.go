package github

import (
	"context"
	"strings"

	gh "github.com/google/go-github/v90/github"

	"github.com/sgaunet/forgectl/internal/forge"
)

// SetDefaultBranch makes name the repository's default branch (FR-037, FR-038).
func (c *Client) SetDefaultBranch(ctx context.Context, name string) error {
	_, _, err := c.api.Repositories.Edit(ctx, c.owner, c.repo, &gh.Repository{
		DefaultBranch: gh.Ptr(name),
	})
	if err != nil {
		return c.wrap("setting the default branch to "+name, err)
	}

	return nil
}

// SetProtection puts the wanted protection in force on a branch, through the
// ruleset forgectl owns (FR-037).
//
// It creates that ruleset when it does not exist and updates it when it does. A
// ruleset carrying any other name is never touched: forgectl is responsible for
// its own, and overwriting a maintainer's would be the kind of surprise a
// check/apply tool exists to avoid.
func (c *Client) SetProtection(ctx context.Context, branch string, want forge.Protection) error {
	return c.applyRuleset(ctx, gh.RulesetTargetBranch, []string{"refs/heads/" + branch},
		rulesFor(want.AllowForcePush, want.AllowDelete))
}

// ProtectTag protects one tag pattern through the tag ruleset forgectl owns
// (FR-025).
//
// The pattern is added to the includes already there rather than replacing
// them, so protecting a second pattern does not unprotect the first.
func (c *Client) ProtectTag(ctx context.Context, pattern string) error {
	existing, err := c.ours(ctx, gh.RulesetTargetTag)
	if err != nil {
		return err
	}

	includes := []string{qualifyTag(pattern)}
	if existing != nil {
		includes = mergeIncludes(refIncludes(existing), includes...)
	}

	// A protected tag denies deletion and force-push, the same two rules that
	// express branch protection (R2).
	return c.applyRuleset(ctx, gh.RulesetTargetTag, includes, rulesFor(false, false))
}

// applyRuleset creates or updates the ruleset forgectl owns for a target.
func (c *Client) applyRuleset(
	ctx context.Context, target gh.RulesetTarget, includes []string, rules *gh.RepositoryRulesetRules,
) error {
	body := gh.RepositoryRuleset{
		Name:        RulesetName,
		Target:      gh.Ptr(target),
		Enforcement: gh.RulesetEnforcementActive,
		Conditions: &gh.RepositoryRulesetConditions{
			RefName: &gh.RepositoryRulesetRefConditionParameters{
				Include: includes,
				Exclude: []string{},
			},
		},
		Rules: rules,
	}

	existing, err := c.ours(ctx, target)
	if err != nil {
		return err
	}

	if existing == nil {
		if _, _, err := c.api.Repositories.CreateRuleset(ctx, c.owner, c.repo, body); err != nil {
			return c.wrap("creating the forgectl ruleset", err)
		}

		return nil
	}

	if _, _, err := c.api.Repositories.UpdateRuleset(
		ctx, c.owner, c.repo, existing.GetID(), body); err != nil {
		return c.wrap("updating the forgectl ruleset", err)
	}

	return nil
}

// rulesFor builds the rule set expressing the wanted permissions.
//
// Omitting a rule PERMITS the action, so allow_force_push: true is expressed by
// leaving non_fast_forward out rather than by setting anything (R2).
func rulesFor(allowForcePush, allowDelete bool) *gh.RepositoryRulesetRules {
	rules := &gh.RepositoryRulesetRules{}

	if !allowForcePush {
		rules.NonFastForward = &gh.EmptyRuleParameters{}
	}
	if !allowDelete {
		rules.Deletion = &gh.EmptyRuleParameters{}
	}

	return rules
}

// qualifyTag turns a bare tag pattern into the fully qualified ref a ruleset
// condition takes, leaving an already-qualified one alone.
func qualifyTag(pattern string) string {
	const prefix = "refs/tags/"

	if strings.HasPrefix(pattern, prefix) {
		return pattern
	}

	return prefix + pattern
}

// mergeIncludes adds patterns to an existing include list, keeping it free of
// duplicates and stable in order.
func mergeIncludes(existing []string, add ...string) []string {
	seen := make(map[string]bool, len(existing)+len(add))
	out := make([]string, 0, len(existing)+len(add))

	for _, pattern := range append(append([]string{}, existing...), add...) {
		if seen[pattern] {
			continue
		}
		seen[pattern] = true
		out = append(out, pattern)
	}

	return out
}
