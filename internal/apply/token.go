package apply

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sgaunet/forgectl/internal/compliance"
	"github.com/sgaunet/forgectl/internal/config"
	"github.com/sgaunet/forgectl/internal/forge"
)

// ErrTokenStranded reports a token that was created while the CI variable write
// that should have consumed it failed.
//
// The token's value is disclosed by the platform exactly once, at creation, so
// a value lost here cannot be recovered — only rotated again. Saying so
// explicitly is the whole of FR-051: the maintainer needs to know that a live
// token now exists which nothing is using, and that rerunning apply is what
// resolves it.
var ErrTokenStranded = errors.New(
	"the token was created but writing its CI variable failed; " +
		"the token remains active and cannot be recovered — rerun apply to rotate again")

// rotateToken creates a project access token, writes it straight into the CI
// variable, and only then revokes the ones it replaced.
//
// The order is the whole point (FR-047, FR-048):
//
//  1. create — the platform discloses the value exactly once
//  2. write  — the CI variable becomes its only persisted copy (FR-050)
//  3. revoke — strictly after the write succeeded
//
// Revoking first, or in parallel, would risk leaving a CI variable holding a
// revoked token: a pipeline that authenticates against nothing. Revoking last
// means an interruption leaves an EXTRA active token instead, which the next
// check reports as ambiguous and the next apply cleans up. One is a nuisance;
// the other is a broken release pipeline.
func (e *Executor) rotateToken(ctx context.Context, action compliance.Action) error {
	def, known := e.Definitions[action.Target]
	if !known || def.Generator == nil {
		return fmt.Errorf("variable %q is not a generated one", action.Target)
	}

	issuer, ok := e.Forge.(forge.TokenIssuer)
	if !ok {
		// This platform cannot issue project tokens. The check already skipped
		// with a warning, so reaching here would be a planning mistake.
		return fmt.Errorf("%w: generated variables need project access tokens",
			forge.ErrNotSupported)
	}

	existing, err := issuer.ProjectTokens(ctx, def.Generator.TokenName)
	if err != nil {
		return fmt.Errorf("listing the tokens to replace: %w", err)
	}

	created, value, err := issuer.CreateProjectToken(ctx, forge.ProjectTokenRequest{
		Name:      def.Generator.TokenName,
		Scopes:    def.Generator.Scopes,
		Role:      def.Generator.Role,
		ExpiresAt: expiryFrom(e.now(), def.Generator.ExpiresIn),
	})
	if err != nil {
		return fmt.Errorf("creating the project access token: %w", err)
	}

	// From here the value exists in exactly one place — this variable — and
	// losing it means losing the token.
	writeErr := e.Forge.SetVariable(ctx, forge.VariableWrite{
		Name:      def.Name,
		Value:     value,
		Secret:    def.Secret,
		Masked:    def.Masked,
		Protected: def.Protected,
	})

	if errors.Is(writeErr, forge.ErrMaskRejected) {
		e.warnf("CI variable %s was written unmasked: %s", def.Name, maskReason(writeErr))
		writeErr = nil
	}

	if writeErr != nil {
		return fmt.Errorf("%w (token id %d)", ErrTokenStranded, created.ID)
	}

	// Only now, with the variable holding the new token, is it safe to revoke
	// what it replaced (FR-048).
	if def.Generator.RevokeRotated {
		e.revokeReplaced(ctx, issuer, existing, created.ID)
	}

	return nil
}

// revokeReplaced revokes every token the new one supersedes.
//
// A revocation that fails is a warning rather than a failure: the CI variable
// already holds the new token, so the run achieved what it set out to do. A
// leftover active token is untidy, and the next check reports it as ambiguous.
func (e *Executor) revokeReplaced(
	ctx context.Context, issuer forge.TokenIssuer, existing []forge.ProjectToken, keep int,
) {
	for _, token := range existing {
		if token.ID == keep {
			continue
		}

		if err := issuer.RevokeProjectToken(ctx, token.ID); err != nil {
			e.warnf("the replaced token %d could not be revoked: %v; "+
				"revoke it by hand or rerun apply", token.ID, err)
		}
	}
}

// expiryFrom computes the expiry date from the configured lifetime. GitLab
// takes a calendar date, so the result is a date rather than an instant.
func expiryFrom(now time.Time, lifetime config.Days) time.Time {
	return now.AddDate(0, 0, int(lifetime))
}

// now returns the executor's clock, which a test replaces to make expiry
// arithmetic deterministic.
func (e *Executor) now() time.Time {
	if e.Now.IsZero() {
		return time.Now()
	}

	return e.Now
}
