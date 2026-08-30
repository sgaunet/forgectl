package github

import (
	"context"
	"fmt"

	gh "github.com/google/go-github/v90/github"

	"github.com/sgaunet/forgectl/internal/forge"
)

// Variable reads what GitHub reports about a CI variable (FR-026).
//
// The attribute mapping is fixed: a secret variable is an Actions secret, a
// non-secret one is an Actions variable. Masking and protection have no GitHub
// equivalent and are left at their zero values, which the compliance layer then
// never compares (FR-026).
func (c *Client) Variable(ctx context.Context, name string, secret bool) (forge.VariableState, error) {
	if secret {
		return c.readSecret(ctx, name)
	}

	return c.readVariable(ctx, name)
}

// readSecret reads an Actions secret's metadata.
//
// GitHub returns metadata only — never the value — so ValueReadable is false
// and FR-027's comparison never runs. That is a fact about the platform, and
// recording it as a field rather than inferring it at the comparison keeps the
// compliance layer free of per-platform special cases.
func (c *Client) readSecret(ctx context.Context, name string) (forge.VariableState, error) {
	_, resp, err := c.api.Actions.GetRepoSecret(ctx, c.owner, c.repo, name)
	if err != nil {
		if isNotFound(resp) {
			return forge.VariableState{}, nil
		}

		return forge.VariableState{}, c.wrap("reading the Actions credential "+name, err)
	}

	return forge.VariableState{Exists: true, ValueReadable: false}, nil
}

// readVariable reads an Actions variable, whose value GitHub does disclose.
func (c *Client) readVariable(ctx context.Context, name string) (forge.VariableState, error) {
	variable, resp, err := c.api.Actions.GetRepoVariable(ctx, c.owner, c.repo, name)
	if err != nil {
		if isNotFound(resp) {
			return forge.VariableState{}, nil
		}

		return forge.VariableState{}, c.wrap("reading the Actions variable "+name, err)
	}

	return forge.VariableState{
		Exists:        true,
		Value:         variable.Value,
		ValueReadable: true,
	}, nil
}

// SetVariable creates or updates a CI variable (FR-042).
//
// A secret's value cannot be read back, so apply writes it on every run rather
// than comparing first. That converges to the same result and is the only thing
// a write-only store allows.
func (c *Client) SetVariable(ctx context.Context, write forge.VariableWrite) error {
	if write.Secret {
		return c.writeSecret(ctx, write)
	}

	return c.writeVariable(ctx, write)
}

// writeSecret seals the value to the repository's public key and stores it.
func (c *Client) writeSecret(ctx context.Context, write forge.VariableWrite) error {
	key, _, err := c.api.Actions.GetRepoPublicKey(ctx, c.owner, c.repo)
	if err != nil {
		return c.wrap("reading the repository public key", err)
	}

	ciphertext, err := seal(key.GetKey(), write.Value)
	if err != nil {
		return fmt.Errorf("preparing the Actions credential %s: %w", write.Name, err)
	}

	_, err = c.api.Actions.CreateOrUpdateRepoSecret(ctx, c.owner, c.repo, write.Name,
		gh.SecretRequest{KeyID: key.GetKeyID(), EncryptedValue: ciphertext})
	if err != nil {
		return c.wrap("writing the Actions credential "+write.Name, err)
	}

	return nil
}

// writeVariable creates or updates an Actions variable.
//
// GitHub separates creation from update, so an existing variable is updated and
// an absent one created. Trying the update first and creating on a 404 would
// make the common path — a variable that already exists — the cheap one.
func (c *Client) writeVariable(ctx context.Context, write forge.VariableWrite) error {
	_, resp, err := c.api.Actions.GetRepoVariable(ctx, c.owner, c.repo, write.Name)
	if err != nil {
		if !isNotFound(resp) {
			return c.wrap("reading the Actions variable "+write.Name, err)
		}

		created := gh.ActionsCreateVariableRequest{Name: write.Name, Value: write.Value}
		if _, err := c.api.Actions.CreateRepoVariable(ctx, c.owner, c.repo, created); err != nil {
			return c.wrap("creating the Actions variable "+write.Name, err)
		}

		return nil
	}

	updated := gh.ActionsUpdateVariableRequest{Value: gh.Ptr(write.Value)}
	if _, err := c.api.Actions.UpdateRepoVariable(
		ctx, c.owner, c.repo, write.Name, updated); err != nil {
		return c.wrap("updating the Actions variable "+write.Name, err)
	}

	return nil
}

// Client implements the whole platform interface. It still does NOT implement
// forge.TokenIssuer: GitHub has no project access token, so a generated
// variable is skipped there with a warning (FR-029).
var _ forge.Forge = (*Client)(nil)
