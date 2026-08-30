package compliance

import (
	"context"
	"fmt"
	"strings"

	"github.com/sgaunet/forgectl/internal/config"
	"github.com/sgaunet/forgectl/internal/forge"
)

// EvaluateVariable checks one CI variable for presence and for the attributes
// the platform supports (FR-026).
//
// Two rules govern what is compared:
//
//   - An attribute the platform has no equivalent for is never drift (FR-026).
//     Masking and protection exist on GitLab and not on GitHub, so they are
//     compared only where the platform reports them.
//   - A value the platform cannot disclose is never compared (FR-027). GitHub's
//     Actions credentials are write-only, so a differing value there is
//     undetectable and apply simply writes on every run.
//
// No value reaches the result. A drifted value is reported as the literal
// string "differs", which is the whole of what a maintainer can be told without
// the report becoming the leak it exists to prevent (FR-054).
func EvaluateVariable(
	ctx context.Context,
	f forge.Reader,
	def config.VariableDefinition,
	expected Expected,
	platform config.Platform,
) (CheckResult, error) {
	id := VarCheckID(def.Name)

	state, err := f.Variable(ctx, def.Name, def.Secret)
	if err != nil {
		return CheckResult{}, fmt.Errorf("reading CI variable %s: %w", def.Name, err)
	}

	if !state.Exists {
		return Fail(id, DomainVars, "present", "missing"), nil
	}

	if drift := variableDrift(def, state, expected, platform); len(drift) > 0 {
		return Fail(id, DomainVars, describeWantedVariable(def, platform), strings.Join(drift, ", ")), nil
	}

	return Pass(id, DomainVars), nil
}

// Expected is the value a variable is supposed to hold, as the caller resolved
// it.
//
// It is a struct rather than a bare string so "no value to compare against" is
// distinct from "the empty value", and it is passed in rather than looked up so
// this package never learns about value stores, override files, or precedence
// chains — it only ever answers whether what the platform holds matches what it
// was told to expect.
type Expected struct {
	// Value is what the variable should hold. It is compared and discarded; it
	// reaches no message and no result field (FR-054).
	Value string
	// Known is false when forgectl cannot know the value — a generated one, or
	// one that will be prompted for. Nothing unknown can drift.
	Known bool
}

// variableDrift lists what differs, in the platform's own terms.
func variableDrift(
	def config.VariableDefinition,
	state forge.VariableState,
	expected Expected,
	platform config.Platform,
) []string {
	var drift []string

	// Masking and protection are GitLab attributes. GitHub models neither, and
	// an attribute the platform cannot express must not be reported as drift.
	if platform == config.PlatformGitLab {
		if state.Masked != def.Masked {
			drift = append(drift, "masked "+yesNo(state.Masked))
		}
		if state.Protected != def.Protected {
			drift = append(drift, "protected "+yesNo(state.Protected))
		}
	}

	// The value is compared only where the platform discloses it, and only
	// where forgectl knows what it should be.
	if state.ValueReadable && expected.Known && expected.Value != state.Value {
		drift = append(drift, "differs")
	}

	return drift
}

// describeWantedVariable renders the configured attributes in the same terms
// the drift is reported in, so the two lines read as a pair.
func describeWantedVariable(def config.VariableDefinition, platform config.Platform) string {
	if platform != config.PlatformGitLab {
		return "present"
	}

	return fmt.Sprintf("masked %s, protected %s", yesNo(def.Masked), yesNo(def.Protected))
}

// yesNo renders an attribute flag the way the report reads it.
func yesNo(v bool) string {
	if v {
		return "yes"
	}

	return "no"
}
