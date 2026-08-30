package apply

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/sgaunet/forgectl/internal/compliance"
	"github.com/sgaunet/forgectl/internal/config"
	"github.com/sgaunet/forgectl/internal/forge"
	"github.com/sgaunet/forgectl/internal/gitrepo"
)

// Executor performs a plan. It is the only type in forgectl that changes
// anything, on the platform or locally.
//
// internal/compliance builds plans and never executes one; this package
// executes them and never builds one. That split is what makes `check` provably
// read-only (FR-031).
type Executor struct {
	// Forge is the platform, with its write half.
	Forge forge.Forge
	// WorkCopy is the local clone, for the branch commands.
	WorkCopy *gitrepo.WorkingCopy
	// Config is the declared intent.
	Config *config.Config
	// Values resolves a variable's value at the moment it is written. It is the
	// only route a value takes, and it is never stored on this struct.
	Values ValueResolver
	// Definitions are the selected variables, keyed by name, carrying the
	// attributes each write needs — and, deliberately, no value.
	Definitions map[string]config.VariableDefinition
	// Warn reports a non-fatal condition. It goes to stderr and into the
	// report, never to stdout.
	Warn func(format string, args ...any)
	// Now is the clock a token's expiry is computed from. A zero Now uses
	// time.Now, which is what production does.
	Now time.Time
}

// ValueResolver hands the executor the value for one variable, at the moment it
// is about to be written.
//
// It is an interface rather than a map so a value is produced on demand and
// held for as long as one call, rather than sitting in the executor's state
// where a later log line could reach it (FR-050, FR-054).
type ValueResolver interface {
	Resolve(ctx context.Context, name string) (string, error)
}

// Result is what a whole run did.
type Result struct {
	Actions []compliance.ActionResult
}

// Failed reports whether any action failed, which makes the run a partial
// application and therefore a runtime failure (CLI-002).
func (r Result) Failed() bool {
	for _, a := range r.Actions {
		if a.Status == compliance.ActionFailed {
			return true
		}
	}

	return false
}

// Run executes the plan in order, stopping at the first failure.
//
// Stopping rather than pressing on is deliberate: the order of FR-034 exists
// because each step depends on the one before it, so continuing past a failure
// would attempt work whose precondition is known to be missing. Everything not
// attempted is reported as skipped, and rerunning apply converges from wherever
// it stopped (FR-045).
func (e *Executor) Run(ctx context.Context, plan compliance.Plan) (Result, error) {
	var result Result

	// stopReason is empty while the run is still going. Once set it stays set,
	// so every remaining action reports why it was not attempted rather than
	// the generic reason of whichever action happened to precede it.
	stopReason := ""

	for _, action := range plan.Actions {
		// An interrupted run stops at the current step and reports what
		// completed (CLI-005).
		if stopReason == "" && ctx.Err() != nil {
			stopReason = "not attempted: the run was interrupted"
		}

		if stopReason != "" {
			result.Actions = append(result.Actions, compliance.ActionResult{
				Action: action,
				Status: compliance.ActionSkipped,
				Error:  stopReason,
			})

			continue
		}

		slog.DebugContext(ctx, "applying", "action", action.Kind, "target", action.Target)

		if err := e.perform(ctx, action); err != nil {
			result.Actions = append(result.Actions, compliance.ActionResult{
				Action: action,
				Status: compliance.ActionFailed,
				Error:  err.Error(),
			})
			stopReason = "not attempted: an earlier action failed"

			continue
		}

		result.Actions = append(result.Actions, compliance.ActionResult{
			Action: action,
			Status: compliance.ActionDone,
		})
	}

	return result, nil
}

// perform dispatches one action to the domain that owns it.
func (e *Executor) perform(ctx context.Context, action compliance.Action) error {
	switch action.Kind {
	case compliance.ActionRenameBranch,
		compliance.ActionPushBranch,
		compliance.ActionSetDefaultBranch,
		compliance.ActionSetRemoteHead,
		compliance.ActionDeleteOldBranch:
		return e.performBranch(ctx, action)

	case compliance.ActionSetProtection, compliance.ActionProtectTag:
		return e.performProtection(ctx, action)

	case compliance.ActionSetVariable, compliance.ActionRotateToken:
		return e.performVariable(ctx, action)

	default:
		return fmt.Errorf("no handler for action %q", action.Kind)
	}
}

// warn emits a non-fatal condition, tolerating an executor built without one.
func (e *Executor) warnf(format string, args ...any) {
	if e.Warn == nil {
		return
	}

	e.Warn(format, args...)
}

// ErrNoResolver reports an executor asked to write a variable without a value
// resolver. It is a programming error rather than a user-facing one.
var ErrNoResolver = errors.New("no value resolver was given")
