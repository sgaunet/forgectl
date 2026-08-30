package compliance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sgaunet/forgectl/internal/config"
	"github.com/sgaunet/forgectl/internal/forge"
)

// EvaluateGenerated checks a variable whose value forgectl generates and
// rotates: a GitLab project access token (FR-028).
//
// The check has five outcomes, and each is a different thing to tell the
// maintainer:
//
//   - no token of that name — the pipeline has no credential at all
//   - more than one — ambiguous; forgectl will not guess which to rotate
//   - expiring within the rotation threshold — states the days remaining
//   - a healthy token whose CI variable is missing — the token exists but no
//     pipeline can reach it
//   - otherwise, a pass carrying the expiry and the days remaining
//
// Two conditions are SKIPS rather than failures. A platform with no project
// access token at all is one (FR-029), and a credential without the right to
// list or create them is the other (FR-030): neither is drift in the
// repository, and reporting them as such would make a healthy project look
// broken.
func EvaluateGenerated(
	ctx context.Context,
	f forge.Reader,
	def config.VariableDefinition,
	now time.Time,
) (CheckResult, error) {
	id := VarCheckID(def.Name)
	gen := def.Generator

	issuer, ok := f.(forge.TokenIssuer)
	if !ok {
		// GitHub has no project access token equivalent. The run does not fail
		// (FR-029).
		return Skip(id, DomainVars,
			"generated variables need project access tokens, which this platform does not have"), nil
	}

	tokens, err := issuer.ProjectTokens(ctx, gen.TokenName)
	if err != nil {
		if errors.Is(err, forge.ErrInsufficientRights) {
			return Skip(id, DomainVars,
				"the credential may not list project access tokens"), nil
		}

		return CheckResult{}, fmt.Errorf("listing project access tokens: %w", err)
	}

	return generatedResult(ctx, f, def, tokens, now)
}

// generatedResult judges the tokens found against the configured lifecycle.
func generatedResult(
	ctx context.Context,
	f forge.Reader,
	def config.VariableDefinition,
	tokens []forge.ProjectToken,
	now time.Time,
) (CheckResult, error) {
	id := VarCheckID(def.Name)
	gen := def.Generator

	// Every result for a generated variable carries a generator status, even
	// when there is no token to describe. It is what marks the variable as
	// generated, and the plan builder reads that marker to decide between
	// rotating a token and writing a plain variable — so leaving it off a
	// "token missing" failure would plan the wrong action entirely.
	switch {
	case len(tokens) == 0:
		result := Fail(id, DomainVars, "one active token", "token missing")
		result.Generator = &GeneratorStatus{Kind: gen.Kind}

		return result, nil

	case len(tokens) > 1:
		// forgectl will not guess which of several tokens is the live one.
		// Rotation resolves it, leaving exactly one (FR-049).
		result := Fail(id, DomainVars, "one active token",
			fmt.Sprintf("ambiguous: %d active tokens named %q; rotate to resolve",
				len(tokens), gen.TokenName))
		result.Generator = &GeneratorStatus{Kind: gen.Kind}

		return result, nil
	}

	token := tokens[0]
	remaining := token.DaysRemaining(now)

	status := &GeneratorStatus{
		Kind:         gen.Kind,
		ExpiresAt:    token.ExpiresAt,
		RotateInDays: remaining - int(gen.RotateBefore),
	}

	if remaining <= int(gen.RotateBefore) {
		result := Fail(id, DomainVars,
			fmt.Sprintf("more than %s of lifetime remaining", gen.RotateBefore),
			fmt.Sprintf("expires in %d days", remaining))
		result.Generator = status

		return result, nil
	}

	// A live token whose CI variable is absent is drift: the token exists, but
	// no pipeline can reach it.
	state, err := f.Variable(ctx, def.Name, def.Secret)
	if err != nil {
		return CheckResult{}, fmt.Errorf("reading CI variable %s: %w", def.Name, err)
	}

	if !state.Exists {
		result := Fail(id, DomainVars, "present", "missing")
		result.Generator = status

		return result, nil
	}

	result := Pass(id, DomainVars)
	result.Generator = status

	return result, nil
}
