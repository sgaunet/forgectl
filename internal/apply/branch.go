package apply

import (
	"context"
	"fmt"

	"github.com/sgaunet/forgectl/internal/compliance"
)

// performBranch executes one step of the default-branch convergence.
//
// The four shapes of FR-037 through FR-039 are decided when the plan is built,
// not here: this function performs the step it is handed and nothing more,
// which is what lets a rerun pick up from wherever the last one stopped.
func (e *Executor) performBranch(ctx context.Context, action compliance.Action) error {
	switch action.Kind {
	case compliance.ActionRenameBranch:
		return e.renameBranch(ctx, action)

	case compliance.ActionPushBranch:
		if err := e.WorkCopy.PushWithUpstream(ctx, action.Target); err != nil {
			return fmt.Errorf("pushing %s: %w", action.Target, err)
		}

		return nil

	case compliance.ActionSetDefaultBranch:
		if err := e.Forge.SetDefaultBranch(ctx, action.Target); err != nil {
			return fmt.Errorf("setting the platform default branch: %w", err)
		}

		// FR-040: the maintainer must be told what the switch did not do.
		e.warnAboutRetargeting(action)

		return nil

	case compliance.ActionSetRemoteHead:
		if err := e.WorkCopy.SetRemoteHead(ctx, action.Target); err != nil {
			return fmt.Errorf("updating the local remote head: %w", err)
		}

		return nil

	case compliance.ActionDeleteOldBranch:
		if err := e.WorkCopy.DeleteRemoteBranch(ctx, action.Target); err != nil {
			return fmt.Errorf("deleting the old remote branch: %w", err)
		}

		return nil

	default:
		return fmt.Errorf("no branch handler for action %q", action.Kind)
	}
}

// renameBranch renames the local branch, creating it from the remote branch
// when it is absent locally (FR-037).
//
// A maintainer may be running forgectl from a clone that never checked the old
// branch out, so "rename" has to mean "make the new name exist locally,
// pointing where the remote one points".
func (e *Executor) renameBranch(ctx context.Context, action compliance.Action) error {
	hasOld, err := e.WorkCopy.LocalBranchExists(ctx, action.From)
	if err != nil {
		return fmt.Errorf("looking for the local branch %s: %w", action.From, err)
	}

	if hasOld {
		if err := e.WorkCopy.RenameBranch(ctx, action.From, action.Target); err != nil {
			return fmt.Errorf("renaming %s: %w", action.From, err)
		}

		return nil
	}

	if err := e.WorkCopy.CreateBranchFromRemote(ctx, action.Target, action.From); err != nil {
		return fmt.Errorf("creating %s from the remote branch: %w", action.Target, err)
	}

	return nil
}

// warnAboutRetargeting states what switching the default branch did NOT do:
// open merge or pull requests still target the old branch, and every other
// clone needs a command run in it (FR-040).
func (e *Executor) warnAboutRetargeting(action compliance.Action) {
	if action.From == "" {
		return
	}

	e.warnf("the default branch is now %s; open merge or pull requests targeting %s "+
		"need retargeting by hand", action.Target, action.From)
	e.warnf("every other clone should run: %s",
		e.WorkCopy.RetargetHint(action.From, action.Target))
}
