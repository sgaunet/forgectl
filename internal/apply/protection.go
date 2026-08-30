package apply

import (
	"context"
	"fmt"

	"github.com/sgaunet/forgectl/internal/compliance"
	"github.com/sgaunet/forgectl/internal/forge"
)

// performProtection executes one step of the protection convergence, branch or
// tag. Both belong to the protection domain (FR-036).
func (e *Executor) performProtection(ctx context.Context, action compliance.Action) error {
	switch action.Kind {
	case compliance.ActionSetProtection:
		want := forge.Protection{
			Exists:          true,
			AllowForcePush:  e.Config.BranchProtection.AllowForcePush,
			AllowDelete:     e.Config.BranchProtection.AllowDelete,
			PushAccessLevel: e.Config.BranchProtection.PushAccessLevel,
		}

		if err := e.Forge.SetProtection(ctx, action.Target, want); err != nil {
			return fmt.Errorf("protecting branch %s: %w", action.Target, err)
		}

		return nil

	case compliance.ActionProtectTag:
		if err := e.Forge.ProtectTag(ctx, action.Target); err != nil {
			return fmt.Errorf("protecting tag pattern %s: %w", action.Target, err)
		}

		return nil

	default:
		return fmt.Errorf("no protection handler for action %q", action.Kind)
	}
}
