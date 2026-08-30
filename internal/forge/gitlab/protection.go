package gitlab

import (
	"context"
	"net/http"

	gl "gitlab.com/gitlab-org/api/client-go"

	"github.com/sgaunet/forgectl/internal/forge"
)

// SetDefaultBranch makes name the project's default branch (FR-037, FR-038).
func (c *Client) SetDefaultBranch(ctx context.Context, name string) error {
	_, _, err := c.api.Projects.EditProject(c.project, &gl.EditProjectOptions{
		DefaultBranch: gl.Ptr(name),
	}, gl.WithContext(ctx))
	if err != nil {
		return c.wrap("setting the default branch to "+name, err)
	}

	return nil
}

// SetProtection puts the wanted protection in force on a branch (FR-037).
//
// A branch with no protection entry is protected with POST; one that already
// has an entry is amended with PATCH. Unprotecting and reprotecting would work
// too, but it would leave the branch briefly unprotected, which is precisely
// the state this tool exists to prevent.
//
// allow_delete is not sent: GitLab always denies deleting a protected branch
// and offers no toggle (R9).
func (c *Client) SetProtection(ctx context.Context, branch string, want forge.Protection) error {
	_, resp, err := c.api.ProtectedBranches.GetProtectedBranch(
		c.project, branch, gl.WithContext(ctx))

	if err != nil && isNotFound(resp) {
		return c.protectBranch(ctx, branch, want)
	}
	if err != nil {
		return c.wrap("reading protection on branch "+branch, err)
	}

	return c.updateProtection(ctx, branch, want)
}

// protectBranch creates the protection entry for a branch that has none.
func (c *Client) protectBranch(ctx context.Context, branch string, want forge.Protection) error {
	level := toGitLabLevel(want.PushAccessLevel)

	_, _, err := c.api.ProtectedBranches.ProtectRepositoryBranches(c.project,
		&gl.ProtectRepositoryBranchesOptions{
			Name:             gl.Ptr(branch),
			AllowForcePush:   gl.Ptr(want.AllowForcePush),
			PushAccessLevel:  gl.Ptr(level),
			MergeAccessLevel: gl.Ptr(gl.DeveloperPermissions),
		}, gl.WithContext(ctx))
	if err != nil {
		return c.wrap("protecting branch "+branch, err)
	}

	return nil
}

// updateProtection amends the protection entry a branch already has.
func (c *Client) updateProtection(ctx context.Context, branch string, want forge.Protection) error {
	level := toGitLabLevel(want.PushAccessLevel)

	_, _, err := c.api.ProtectedBranches.UpdateProtectedBranch(c.project, branch,
		&gl.UpdateProtectedBranchOptions{
			AllowForcePush: gl.Ptr(want.AllowForcePush),
			AllowedToPush: &[]*gl.BranchPermissionOptions{
				{AccessLevel: gl.Ptr(level)},
			},
		}, gl.WithContext(ctx))
	if err != nil {
		return c.wrap("updating protection on branch "+branch, err)
	}

	return nil
}

// ProtectTag protects one tag pattern (FR-025).
//
// A pattern that is already protected is left alone rather than re-protected:
// GitLab rejects a duplicate, and reporting that as a failure would make apply
// non-idempotent.
func (c *Client) ProtectTag(ctx context.Context, pattern string) error {
	_, resp, err := c.api.ProtectedTags.ProtectRepositoryTags(c.project,
		&gl.ProtectRepositoryTagsOptions{
			Name:              gl.Ptr(pattern),
			CreateAccessLevel: gl.Ptr(gl.MaintainerPermissions),
		}, gl.WithContext(ctx))
	if err != nil {
		// A pattern that is already protected is the state apply wanted, so a
		// conflict is success. Reporting it as a failure would make a second
		// apply fail on a repository the first one converged (FR-035).
		if isConflict(resp) {
			return nil
		}

		return c.wrap("protecting tag pattern "+pattern, err)
	}

	return nil
}

// isConflict reports whether a response is GitLab's "that already exists".
func isConflict(resp *gl.Response) bool {
	return resp != nil && resp.StatusCode == http.StatusConflict
}
