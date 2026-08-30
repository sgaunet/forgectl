package gitlab

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	gl "gitlab.com/gitlab-org/api/client-go"

	"github.com/sgaunet/forgectl/internal/forge"
)

// lifetimeRejection is the fragment GitLab's own message carries when the
// requested expiry exceeds the instance maximum (R8). Matching on it lets the
// limit be reported as the distinct condition FR-052 describes rather than as a
// generic validation failure.
const lifetimeRejection = "expiration date must be within"

// ProjectTokens lists the ACTIVE tokens carrying the given name (FR-028).
//
// Only active, unrevoked tokens count. A revoked token of the same name is
// history, not a duplicate, and counting it would make every project that has
// ever rotated report itself as ambiguous.
func (c *Client) ProjectTokens(ctx context.Context, name string) ([]forge.ProjectToken, error) {
	var found []forge.ProjectToken

	opts := &gl.ListProjectAccessTokensOptions{
		ListOptions: gl.ListOptions{PerPage: perPage},
	}

	for {
		page, resp, err := c.api.ProjectAccessTokens.ListProjectAccessTokens(
			c.project, opts, gl.WithContext(ctx))
		if err != nil {
			return nil, c.wrapTokenError("listing project access tokens", err)
		}

		for _, tok := range page {
			if tok.Name != name || !tok.Active || tok.Revoked {
				continue
			}
			found = append(found, toProjectToken(tok))
		}

		if resp == nil || resp.NextPage == 0 {
			return found, nil
		}
		opts.Page = resp.NextPage
	}
}

// CreateProjectToken creates a project access token and returns it with its
// value (FR-047).
//
// The value is returned ONLY here, by the platform, exactly once (R8). It is
// handed straight back to the caller, which writes it into the CI variable
// immediately and keeps no copy. That is why FR-051 exists: a failure between
// this call and that write cannot be recovered, only rotated again.
//
// expires_at is always sent explicitly rather than relying on the instance
// default, so the lifetime a maintainer configured is the one they get.
func (c *Client) CreateProjectToken(
	ctx context.Context, req forge.ProjectTokenRequest,
) (forge.ProjectToken, string, error) {
	expires := gl.ISOTime(req.ExpiresAt)
	level := toGitLabLevel(req.Role)
	scopes := append([]string(nil), req.Scopes...)

	created, _, err := c.api.ProjectAccessTokens.CreateProjectAccessToken(c.project,
		&gl.CreateProjectAccessTokenOptions{
			Name:        gl.Ptr(req.Name),
			Scopes:      &scopes,
			AccessLevel: gl.Ptr(level),
			ExpiresAt:   &expires,
		}, gl.WithContext(ctx))
	if err != nil {
		return forge.ProjectToken{}, "", c.wrapTokenError("creating a project access token", err)
	}

	return toProjectToken(created), created.Token, nil
}

// RevokeProjectToken revokes one token by id (FR-048).
func (c *Client) RevokeProjectToken(ctx context.Context, id int) error {
	_, err := c.api.ProjectAccessTokens.RevokeProjectAccessToken(
		c.project, int64(id), gl.WithContext(ctx))
	if err != nil {
		return c.wrapTokenError(fmt.Sprintf("revoking project access token %d", id), err)
	}

	return nil
}

// toProjectToken maps the client's token onto forgectl's own.
func toProjectToken(tok *gl.ProjectAccessToken) forge.ProjectToken {
	out := forge.ProjectToken{
		ID:      int(tok.ID),
		Name:    tok.Name,
		Active:  tok.Active,
		Revoked: tok.Revoked,
	}

	if tok.ExpiresAt != nil {
		out.ExpiresAt = time.Time(*tok.ExpiresAt)
	}

	return out
}

// wrapTokenError classifies the two failures the token lifecycle has to tell
// apart from everything else.
//
// A lifetime above the instance maximum is surfaced with the platform's own
// wording, which states the permitted maximum (FR-052). A missing right becomes
// ErrInsufficientRights, which makes the affected check SKIP rather than fail:
// a credential that cannot manage tokens has not found drift, it has found the
// limits of its own permissions (FR-030).
func (c *Client) wrapTokenError(what string, err error) error {
	var apiErr *gl.ErrorResponse
	if errors.As(err, &apiErr) {
		if strings.Contains(strings.ToLower(apiErr.Message), lifetimeRejection) {
			return fmt.Errorf("%w: %s", forge.ErrTokenLifetime, apiErr.Message)
		}
	}

	return c.wrap(what, err)
}

// Client issues project access tokens, which is what makes a generated variable
// possible on GitLab and impossible on GitHub (FR-029).
var _ forge.TokenIssuer = (*Client)(nil)
