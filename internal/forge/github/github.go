package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	gh "github.com/google/go-github/v90/github"

	"github.com/sgaunet/forgectl/internal/forge"
)

// RulesetName is the name of the ruleset forgectl owns.
//
// forgectl creates rulesets under this name and modifies only those. A ruleset
// carrying any other name is left untouched and, if it already grants the
// required protection, the check still passes: forgectl verifies the effect,
// not its own authorship (research.md open item 2).
const RulesetName = "forgectl"

// perPage is the page size used for every listing.
const perPage = 100

// Client implements forge.Forge against the GitHub REST API.
//
// It deliberately does NOT implement forge.TokenIssuer: GitHub has no project
// access token equivalent, so a generated variable is skipped with a warning
// rather than failed (FR-029). The absence of those methods makes that skip a
// fact about the type rather than a runtime check.
type Client struct {
	api   *gh.Client
	owner string
	repo  string
}

// New builds a client for one repository, carrying forgectl's own transport:
// the 30-second timeout and the bounded, backed-off retry (CLI-005, R5).
func New(target forge.Target) (*Client, error) {
	opts := []gh.ClientOptionsFunc{
		// The transport is forgectl's, not the library's: it owns the timeout
		// and the bounded retry, so the retry policy is ours to test.
		gh.WithHTTPClient(forge.NewClient()),
		gh.WithAuthToken(target.Credential),
	}

	// The public host needs no base URL; anything else is GitHub Enterprise.
	if target.Instance.Host != "github.com" && target.Instance.APIURL != "" {
		opts = append(opts, gh.WithEnterpriseURLs(target.Instance.APIURL, target.Instance.APIURL))
	}

	api, err := gh.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("building the GitHub client: %w", err)
	}

	return &Client{api: api, owner: target.Owner, repo: target.Repo}, nil
}

// NewAt builds a client against an explicit API base URL, which is how the
// httptest suite points it at its own server.
func NewAt(baseURL, owner, repo string) (*Client, error) {
	api, err := gh.NewClient(gh.WithEnterpriseURLs(baseURL, baseURL))
	if err != nil {
		return nil, fmt.Errorf("configuring the GitHub API base URL: %w", err)
	}

	return &Client{api: api, owner: owner, repo: repo}, nil
}

// DefaultBranch reports the repository's default branch (FR-023).
func (c *Client) DefaultBranch(ctx context.Context) (string, error) {
	repo, _, err := c.api.Repositories.Get(ctx, c.owner, c.repo)
	if err != nil {
		return "", c.wrap("reading the repository", err)
	}

	return repo.GetDefaultBranch(), nil
}

// BranchExists reports whether the named branch is on the platform. A 404 is
// the answer "no", not a failure (FR-024).
func (c *Client) BranchExists(ctx context.Context, name string) (bool, error) {
	_, resp, err := c.api.Repositories.GetBranch(ctx, c.owner, c.repo, name, 0)
	if err != nil {
		if isNotFound(resp) {
			return false, nil
		}

		return false, c.wrap("reading branch "+name, err)
	}

	return true, nil
}

// Protection reads the protection in force on a branch, through rulesets.
//
// Every ACTIVE ruleset covering the branch is considered, not only the one
// forgectl owns: a maintainer who wrote their own ruleset granting the required
// protection is compliant, and reporting drift at them would be wrong.
func (c *Client) Protection(ctx context.Context, branch string) (forge.Protection, error) {
	covering, err := c.rulesetsCovering(ctx, gh.RulesetTargetBranch, "refs/heads/"+branch)
	if err != nil {
		return forge.Protection{}, err
	}

	if len(covering) == 0 {
		return forge.Protection{}, nil
	}

	var deletionDenied, forcePushDenied bool
	for _, set := range covering {
		if set.Rules == nil {
			continue
		}
		if set.Rules.Deletion != nil {
			deletionDenied = true
		}
		if set.Rules.NonFastForward != nil {
			forcePushDenied = true
		}
	}

	return forge.Protection{
		Exists: true,
		// A rule that is absent permits the action, so allow_force_push: true in
		// configuration means simply omitting non_fast_forward (R2).
		AllowForcePush: !forcePushDenied,
		AllowDelete:    !deletionDenied,
		// GitHub models no push access level; the zero value is never compared
		// (FR-026).
	}, nil
}

// TagProtection lists the tag patterns any active ruleset protects (FR-025).
func (c *Client) TagProtection(ctx context.Context) ([]string, error) {
	sets, err := c.rulesets(ctx)
	if err != nil {
		return nil, err
	}

	var patterns []string

	for _, summary := range sets {
		if summary.Target == nil || *summary.Target != gh.RulesetTargetTag {
			continue
		}

		full, err := c.ruleset(ctx, summary.GetID())
		if err != nil {
			return nil, err
		}
		if full.Enforcement != gh.RulesetEnforcementActive {
			continue
		}

		// Only a ruleset that actually denies deletion or force-push counts as
		// protecting the pattern; one that merely names it protects nothing.
		if full.Rules == nil || (full.Rules.Deletion == nil && full.Rules.NonFastForward == nil) {
			continue
		}

		patterns = append(patterns, refIncludes(full)...)
	}

	return patterns, nil
}

// rulesetsCovering returns the full, active rulesets of a target that apply to
// the given ref.
func (c *Client) rulesetsCovering(
	ctx context.Context, target gh.RulesetTarget, ref string,
) ([]*gh.RepositoryRuleset, error) {
	sets, err := c.rulesets(ctx)
	if err != nil {
		return nil, err
	}

	var covering []*gh.RepositoryRuleset

	for _, summary := range sets {
		if summary.Target == nil || *summary.Target != target {
			continue
		}

		full, err := c.ruleset(ctx, summary.GetID())
		if err != nil {
			return nil, err
		}
		if full.Enforcement != gh.RulesetEnforcementActive {
			continue
		}
		if !includesRef(full, ref) {
			continue
		}

		covering = append(covering, full)
	}

	return covering, nil
}

// rulesets lists the repository's rulesets, following pagination. The listing
// returns summaries: conditions and rules come only from GetRuleset.
func (c *Client) rulesets(ctx context.Context) ([]*gh.RepositoryRuleset, error) {
	var all []*gh.RepositoryRuleset

	opts := &gh.RepositoryListRulesetsOptions{ListOptions: gh.ListOptions{PerPage: perPage}}
	for {
		page, resp, err := c.api.Repositories.GetAllRulesets(ctx, c.owner, c.repo, opts)
		if err != nil {
			return nil, c.wrap("listing repository rulesets", err)
		}
		all = append(all, page...)

		if resp == nil || resp.NextPage == 0 {
			return all, nil
		}
		opts.Page = resp.NextPage
	}
}

// ruleset reads one ruleset in full.
func (c *Client) ruleset(ctx context.Context, id int64) (*gh.RepositoryRuleset, error) {
	full, _, err := c.api.Repositories.GetRuleset(ctx, c.owner, c.repo, id, false)
	if err != nil {
		return nil, c.wrap("reading a repository ruleset", err)
	}

	return full, nil
}

// ours finds the ruleset forgectl owns for a target, or nil when it has not
// created one yet. A ruleset carrying another name is never returned, and so is
// never modified.
func (c *Client) ours(ctx context.Context, target gh.RulesetTarget) (*gh.RepositoryRuleset, error) {
	sets, err := c.rulesets(ctx)
	if err != nil {
		return nil, err
	}

	for _, summary := range sets {
		if summary.Name != RulesetName {
			continue
		}
		if summary.Target == nil || *summary.Target != target {
			continue
		}

		return c.ruleset(ctx, summary.GetID())
	}

	return nil, nil //nolint:nilnil // "no ruleset of ours yet" is a normal answer
}

// refIncludes returns the ref-name patterns a ruleset applies to.
func refIncludes(set *gh.RepositoryRuleset) []string {
	if set == nil || set.Conditions == nil || set.Conditions.RefName == nil {
		return nil
	}

	return set.Conditions.RefName.Include
}

// includesRef reports whether a ruleset's conditions cover the given ref.
// GitHub's own aliases ~ALL and ~DEFAULT_BRANCH count as covering it.
func includesRef(set *gh.RepositoryRuleset, ref string) bool {
	for _, include := range refIncludes(set) {
		if include == ref || include == "~ALL" || include == "~DEFAULT_BRANCH" {
			return true
		}
	}

	return false
}

// isNotFound reports whether a response is GitHub's "no such thing".
func isNotFound(resp *gh.Response) bool {
	return resp != nil && resp.StatusCode == http.StatusNotFound
}

// wrap turns a client error into one forgectl can classify. It never includes
// the credential, only the platform's own message (FR-054).
func (c *Client) wrap(what string, err error) error {
	var apiErr *gh.ErrorResponse
	if errors.As(err, &apiErr) && apiErr.Response != nil {
		if apiErr.Response.StatusCode == http.StatusUnauthorized ||
			apiErr.Response.StatusCode == http.StatusForbidden {
			return fmt.Errorf("%s: %w: %s", what, forge.ErrInsufficientRights, apiErr.Message)
		}
	}

	return fmt.Errorf("%s: %w", what, err)
}
