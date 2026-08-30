package gitlab

import (
	"context"
	"errors"
	"fmt"
	"strings"

	gl "gitlab.com/gitlab-org/api/client-go"

	"github.com/sgaunet/forgectl/internal/forge"
)

// Variable reads what GitLab reports about a CI variable (FR-026).
//
// GitLab discloses the value, so ValueReadable is true and a differing value is
// genuine drift (FR-027). The masked and protected attributes are carried
// directly, because GitLab models both.
func (c *Client) Variable(ctx context.Context, name string, _ bool) (forge.VariableState, error) {
	variable, resp, err := c.api.ProjectVariables.GetVariable(
		c.project, name, nil, gl.WithContext(ctx))
	if err != nil {
		if isNotFound(resp) {
			return forge.VariableState{}, nil
		}

		return forge.VariableState{}, c.wrap("reading CI variable "+name, err)
	}

	return forge.VariableState{
		Exists:        true,
		Masked:        variable.Masked,
		Protected:     variable.Protected,
		Value:         variable.Value,
		ValueReadable: true,
	}, nil
}

// SetVariable creates or updates a CI variable (FR-042).
//
// When GitLab refuses to mask the value, the write is retried once unmasked and
// a warning names the constraint (FR-043). The retry keys on the CLASS of
// rejection rather than on the value being multiline: GitLab rejects a masked
// value that is multiline, that contains a space, OR that is shorter than eight
// characters, and an SSH private key trips the first two (R7).
//
// `protected` is never downgraded. Masking hides a value in job logs;
// protection decides which refs can see it at all. Silently widening the second
// to work around a limit on the first would be a security decision forgectl has
// no business making on its own.
func (c *Client) SetVariable(ctx context.Context, write forge.VariableWrite) error {
	err := c.writeVariable(ctx, write, write.Masked)
	if err == nil {
		return nil
	}

	constraint, rejected := maskingConstraint(err)
	if !rejected || !write.Masked {
		return err
	}

	if retryErr := c.writeVariable(ctx, write, false); retryErr != nil {
		return retryErr
	}

	// The warning names the constraint, never the value.
	return fmt.Errorf("%w: %s", forge.ErrMaskRejected, constraint)
}

// writeVariable performs one create-or-update with an explicit masked flag.
func (c *Client) writeVariable(ctx context.Context, write forge.VariableWrite, masked bool) error {
	_, resp, err := c.api.ProjectVariables.GetVariable(
		c.project, write.Name, nil, gl.WithContext(ctx))

	if err != nil && isNotFound(resp) {
		return c.createVariable(ctx, write, masked)
	}
	if err != nil {
		return c.wrap("reading CI variable "+write.Name, err)
	}

	return c.updateVariable(ctx, write, masked)
}

// createVariable creates a CI variable that does not exist yet.
func (c *Client) createVariable(ctx context.Context, write forge.VariableWrite, masked bool) error {
	_, _, err := c.api.ProjectVariables.CreateVariable(c.project, &gl.CreateProjectVariableOptions{
		Key:       gl.Ptr(write.Name),
		Value:     gl.Ptr(write.Value),
		Masked:    gl.Ptr(masked),
		Protected: gl.Ptr(write.Protected),
	}, gl.WithContext(ctx))
	if err != nil {
		return c.wrap("creating CI variable "+write.Name, err)
	}

	return nil
}

// updateVariable overwrites a CI variable that already exists.
func (c *Client) updateVariable(ctx context.Context, write forge.VariableWrite, masked bool) error {
	_, _, err := c.api.ProjectVariables.UpdateVariable(
		c.project, write.Name, &gl.UpdateProjectVariableOptions{
			Value:     gl.Ptr(write.Value),
			Masked:    gl.Ptr(masked),
			Protected: gl.Ptr(write.Protected),
		}, gl.WithContext(ctx))
	if err != nil {
		return c.wrap("updating CI variable "+write.Name, err)
	}

	return nil
}

// maskingConstraints are the three rules GitLab enforces on a masked value, and
// the fragment of its rejection message that identifies each (R7).
//
// The spec framed this as a multiline problem alone. It is not: implementing
// the retry against that one condition would leave a short or space-bearing
// value failing hard, which is exactly what the ssh-deploy profile would hit.
var maskingConstraints = []struct {
	fragment string
	says     string
}{
	{fragment: "single line", says: "a masked value must be a single line"},
	{fragment: "space", says: "a masked value must contain no spaces"},
	{fragment: "8 characters", says: "a masked value must be at least 8 characters"},
	{fragment: "masking", says: "the value does not satisfy GitLab's masking requirements"},
	{fragment: "masked", says: "the value does not satisfy GitLab's masking requirements"},
}

// maskingConstraint reports whether an error is GitLab refusing to mask, and
// which constraint it names.
func maskingConstraint(err error) (string, bool) {
	var apiErr *gl.ErrorResponse
	if !errors.As(err, &apiErr) {
		return "", false
	}

	message := strings.ToLower(apiErr.Message)
	for _, constraint := range maskingConstraints {
		if strings.Contains(message, constraint.fragment) {
			return constraint.says, true
		}
	}

	return "", false
}

// Client implements the whole platform interface.
var _ forge.Forge = (*Client)(nil)
