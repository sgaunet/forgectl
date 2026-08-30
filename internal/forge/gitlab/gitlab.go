package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	gl "gitlab.com/gitlab-org/api/client-go"

	"github.com/sgaunet/forgectl/internal/config"
	"github.com/sgaunet/forgectl/internal/forge"
)

// perPage is the page size used for every listing.
const perPage = 100

// Client implements forge.Forge against the GitLab REST API, and additionally
// forge.TokenIssuer: GitLab is the only platform with project access tokens,
// which is why the lifecycle is not on the shared interface (FR-029).
type Client struct {
	api *gl.Client
	// project is the URL-encoded owner/repo the API takes as its id.
	project string
}

// New builds a client for one repository, carrying forgectl's own transport:
// the 30-second timeout and the bounded, backed-off retry (CLI-005, R5).
func New(target forge.Target) (*Client, error) {
	api, err := gl.NewClient(target.Credential,
		// The transport is forgectl's, not the library's. The client's own
		// retry is disabled so there is exactly one retry policy in the binary,
		// and it is the one CLI-005 specifies and internal/forge tests.
		gl.WithHTTPClient(forge.NewClient()),
		gl.WithCustomRetryMax(0),
		gl.WithBaseURL(target.Instance.APIURL),
	)
	if err != nil {
		return nil, fmt.Errorf("building the GitLab client: %w", err)
	}

	return &Client{api: api, project: target.Project()}, nil
}

// NewAt builds a client against an explicit API base URL, which is how the
// httptest suite points it at its own server.
func NewAt(baseURL, project string) (*Client, error) {
	api, err := gl.NewClient("test-token", gl.WithBaseURL(baseURL), gl.WithCustomRetryMax(0))
	if err != nil {
		return nil, fmt.Errorf("configuring the GitLab API base URL: %w", err)
	}

	return &Client{api: api, project: project}, nil
}

// DefaultBranch reports the project's default branch (FR-023).
func (c *Client) DefaultBranch(ctx context.Context) (string, error) {
	project, _, err := c.api.Projects.GetProject(c.project, nil, gl.WithContext(ctx))
	if err != nil {
		return "", c.wrap("reading the project", err)
	}

	return project.DefaultBranch, nil
}

// BranchExists reports whether the named branch is on the platform. A 404 is
// the answer "no", not a failure (FR-024).
func (c *Client) BranchExists(ctx context.Context, name string) (bool, error) {
	_, resp, err := c.api.Branches.GetBranch(c.project, name, gl.WithContext(ctx))
	if err != nil {
		if isNotFound(resp) {
			return false, nil
		}

		return false, c.wrap("reading branch "+name, err)
	}

	return true, nil
}

// Protection reads the protection in force on a branch (FR-024).
//
// AllowDelete is reported as false unconditionally: GitLab always denies
// deleting a protected branch and offers no toggle, so the configured
// allow_delete: false is satisfied by the branch being protected at all (R9).
func (c *Client) Protection(ctx context.Context, branch string) (forge.Protection, error) {
	protected, resp, err := c.api.ProtectedBranches.GetProtectedBranch(
		c.project, branch, gl.WithContext(ctx))
	if err != nil {
		if isNotFound(resp) {
			return forge.Protection{}, nil
		}

		return forge.Protection{}, c.wrap("reading protection on branch "+branch, err)
	}

	return forge.Protection{
		Exists:          true,
		AllowForcePush:  protected.AllowForcePush,
		AllowDelete:     false,
		PushAccessLevel: pushAccessLevel(protected),
	}, nil
}

// TagProtection lists the protected tag patterns (FR-025).
func (c *Client) TagProtection(ctx context.Context) ([]string, error) {
	var patterns []string

	opts := &gl.ListProtectedTagsOptions{ListOptions: gl.ListOptions{PerPage: perPage}}
	for {
		page, resp, err := c.api.ProtectedTags.ListProtectedTags(
			c.project, opts, gl.WithContext(ctx))
		if err != nil {
			return nil, c.wrap("listing protected tags", err)
		}

		for _, tag := range page {
			patterns = append(patterns, tag.Name)
		}

		if resp == nil || resp.NextPage == 0 {
			return patterns, nil
		}
		opts.Page = resp.NextPage
	}
}

// pushAccessLevel reduces GitLab's list of push access descriptions to the one
// level forgectl compares. The most permissive wins: it is the level that
// actually governs who can push.
func pushAccessLevel(protected *gl.ProtectedBranch) config.AccessLevel {
	highest := gl.NoPermissions

	for _, access := range protected.PushAccessLevels {
		// A per-user or per-group grant is not a level; it is an exception, and
		// forgectl does not model exceptions.
		if access.UserID != 0 || access.GroupID != 0 || access.DeployKeyID != 0 {
			continue
		}
		if access.AccessLevel > highest {
			highest = access.AccessLevel
		}
	}

	return fromGitLabLevel(highest)
}

// fromGitLabLevel maps a numeric GitLab access level onto the configured name.
// Anything above maintainer is reported as maintainer: forgectl's vocabulary
// stops there, and an owner-level grant satisfies a maintainer requirement.
func fromGitLabLevel(level gl.AccessLevelValue) config.AccessLevel {
	switch {
	case level >= gl.MaintainerPermissions:
		return config.AccessMaintainer
	case level >= gl.DeveloperPermissions:
		return config.AccessDeveloper
	default:
		return config.AccessNone
	}
}

// toGitLabLevel maps a configured access level onto GitLab's numeric one.
func toGitLabLevel(level config.AccessLevel) gl.AccessLevelValue {
	return gl.AccessLevelValue(level.GitLab()) //nolint:gosec // the mapping is 0, 30, or 40
}

// isNotFound reports whether a response is GitLab's "no such thing".
func isNotFound(resp *gl.Response) bool {
	return resp != nil && resp.StatusCode == http.StatusNotFound
}

// wrap turns a client error into one forgectl can classify. It never includes
// the credential, only the platform's own message (FR-054).
//
// A 401 or 403 becomes ErrInsufficientRights, which lets the token checks skip
// with a reason instead of failing the run (FR-030).
func (c *Client) wrap(what string, err error) error {
	var apiErr *gl.ErrorResponse
	if errors.As(err, &apiErr) && apiErr.Response != nil {
		if apiErr.Response.StatusCode == http.StatusUnauthorized ||
			apiErr.Response.StatusCode == http.StatusForbidden {
			return fmt.Errorf("%s: %w", what, forge.ErrInsufficientRights)
		}
	}

	return fmt.Errorf("%s: %w", what, err)
}
