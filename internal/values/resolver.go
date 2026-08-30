package values

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/sgaunet/forgectl/internal/config"
)

// ErrMissingValues reports variables no source could supply. Its message names
// every one of them, so a maintainer fixes them in one edit rather than
// discovering them one run at a time (FR-044).
var ErrMissingValues = errors.New("no value is available")

// Prompter asks a human for a value, with the input concealed. It is nil when
// no terminal is attached, which is what makes FR-044's last resort fail
// cleanly rather than hang.
type Prompter interface {
	// Prompt asks for the named variable's value. The name is shown; the value
	// is never echoed.
	Prompt(name string) (string, error)
}

// Resolver produces the value for one variable at the moment it is written.
type Resolver struct {
	// varFile is the --var-file override, the highest precedence.
	varFile map[string]string
	// store is the shared value store from the configuration.
	store map[string]string
	// definitions are the selected variables, keyed by name.
	definitions map[string]config.VariableDefinition
	// prompter is consulted only when a terminal is attached.
	prompter Prompter
}

// NewResolver builds a resolver over the selected variables.
func NewResolver(
	sel config.Selection, store map[string]string, varFile map[string]string, prompter Prompter,
) *Resolver {
	definitions := make(map[string]config.VariableDefinition, len(sel.Variables))
	for _, v := range sel.Variables {
		definitions[v.Name] = v
	}

	return &Resolver{
		varFile:     varFile,
		store:       store,
		definitions: definitions,
		prompter:    prompter,
	}
}

// Resolve returns the value for one variable, following the order of FR-044:
// the override file, then the inline literal or store reference, then the
// concealed prompt.
//
// A generated variable is deliberately NOT resolved here: its value comes from
// creating a token, which mutates the platform, and that belongs to the
// execution layer rather than to a resolver a read-only path might call.
func (r *Resolver) Resolve(_ context.Context, name string) (string, error) {
	if value, ok := r.varFile[name]; ok {
		return value, nil
	}

	def, known := r.definitions[name]
	if !known {
		return "", fmt.Errorf("%w: %q is not a selected variable", ErrMissingValues, name)
	}

	switch def.Source() {
	case config.SourceLiteral:
		if def.Value != "" {
			return def.Value, nil
		}

	case config.SourceRef:
		if value := r.store[def.ValueRef]; value != "" {
			return value, nil
		}

	case config.SourceGenerator:
		// A generated value comes from creating a token, which mutates the
		// platform. That belongs to the execution layer, not to a resolver a
		// read-only path might call.
		return "", fmt.Errorf("%w: %q is generated, and its value comes from the platform",
			ErrMissingValues, name)
	}

	// The declared source supplied nothing — an empty literal, or a store key
	// declared but left blank. The last resort is to ask, when there is anyone
	// to ask (FR-044).
	return r.prompt(name)
}

// CheckComplete verifies that every variable the run will write can be
// resolved, BEFORE the first write.
//
// This is what stops a missing value from leaving the repository half
// converged: forgectl would otherwise write three variables, discover the
// fourth has no value, and stop — leaving a state no one asked for (FR-044).
//
// The error names every missing variable at once. Generated variables are not
// checked here: their value comes from the platform during execution.
func (r *Resolver) CheckComplete(ctx context.Context, names []string) error {
	var missing []string

	for _, name := range names {
		def, known := r.definitions[name]
		if known && def.Generator != nil {
			continue
		}

		if _, err := r.Resolve(ctx, name); err != nil {
			missing = append(missing, name)
		}
	}

	if len(missing) == 0 {
		return nil
	}

	sort.Strings(missing)

	return fmt.Errorf("%w for: %s; declare them under `values` in the configuration, "+
		"or supply them with --var-file",
		ErrMissingValues, strings.Join(missing, ", "))
}

// Definition returns the declaration of one selected variable.
func (r *Resolver) Definition(name string) (config.VariableDefinition, bool) {
	def, ok := r.definitions[name]

	return def, ok
}

// Overrides returns the per-variable values the override file supplied.
//
// It is what lets apply re-evaluate against the state it was told to produce
// rather than against the configuration it was told to override. The map is
// copied, so a caller cannot reach back into the resolver's own state.
func (r *Resolver) Overrides() map[string]string {
	out := make(map[string]string, len(r.varFile))
	for name, value := range r.varFile {
		out[name] = value
	}

	return out
}

// prompt asks a human, when there is one.
func (r *Resolver) prompt(name string) (string, error) {
	if r.prompter == nil {
		return "", fmt.Errorf("%w: %q, and no terminal is attached to ask for it",
			ErrMissingValues, name)
	}

	value, err := r.prompter.Prompt(name)
	if err != nil {
		return "", fmt.Errorf("asking for %q: %w", name, err)
	}
	if value == "" {
		return "", fmt.Errorf("%w: %q, and the prompt was answered with nothing",
			ErrMissingValues, name)
	}

	return value, nil
}
