package apply

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sgaunet/forgectl/internal/compliance"
	"github.com/sgaunet/forgectl/internal/forge"
)

// performVariable executes one variable write.
func (e *Executor) performVariable(ctx context.Context, action compliance.Action) error {
	switch action.Kind {
	case compliance.ActionSetVariable:
		return e.setVariable(ctx, action)

	case compliance.ActionRotateToken:
		return e.rotateToken(ctx, action)

	default:
		return fmt.Errorf("no variable handler for action %q", action.Kind)
	}
}

// setVariable resolves the value and writes it.
//
// The value is fetched one line before it is needed and passed straight into
// SetVariable. It is not stored on the executor, not logged, and not put into
// any error: this function is the entire lifetime of a value inside the
// execution layer (FR-050, FR-054).
func (e *Executor) setVariable(ctx context.Context, action compliance.Action) error {
	if e.Values == nil {
		return ErrNoResolver
	}

	def, known := e.Definitions[action.Target]
	if !known {
		return fmt.Errorf("variable %q is not among the selected ones", action.Target)
	}

	value, err := e.Values.Resolve(ctx, action.Target)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", action.Target, err)
	}

	err = e.Forge.SetVariable(ctx, forge.VariableWrite{
		Name:      def.Name,
		Value:     value,
		Secret:    def.Secret,
		Masked:    def.Masked,
		Protected: def.Protected,
	})

	// A value the platform refuses to mask was written UNMASKED instead. That
	// is a warning, not a failure: the variable is in place, and refusing to
	// write an SSH key at all because it cannot be masked would be the worse
	// outcome (FR-043, R7).
	if errors.Is(err, forge.ErrMaskRejected) {
		e.warnf("CI variable %s was written unmasked: %s", def.Name, maskReason(err))

		return nil
	}

	if err != nil {
		return fmt.Errorf("writing CI variable %s: %w", def.Name, err)
	}

	return nil
}

// maskReason extracts the constraint the platform named, which the client put
// in the message after the sentinel. It never carries the value.
func maskReason(err error) string {
	const sep = ": "

	msg := err.Error()
	if _, after, found := strings.Cut(msg, forge.ErrMaskRejected.Error()+sep); found {
		return after
	}

	return msg
}
