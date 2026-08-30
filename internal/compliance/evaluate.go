package compliance

import (
	"context"
	"time"

	"github.com/sgaunet/forgectl/internal/config"
	"github.com/sgaunet/forgectl/internal/forge"
)

// Evaluator assembles the check catalog and runs it.
//
// This package is read-only by construction: it imports internal/forge for the
// interface and internal/config for the declared intent, and it never imports
// internal/apply. `check` therefore cannot modify anything, and that is a
// property of the import graph rather than of anyone's discipline (FR-031).
type Evaluator struct {
	// Forge is the platform under inspection, read-only by type.
	Forge forge.Reader
	// Config is the declared intent.
	Config *config.Config
	// Selection is the profiles this run operates on, possibly empty.
	Selection config.Selection
	// Instance is the forge the run targeted, carried into the report.
	Instance config.Instance
	// Repository is owner/name, carried into the report.
	Repository string
	// Overrides are per-variable values that take precedence over the shared
	// value store, keyed by variable name.
	//
	// They exist so that apply, re-evaluating to report the state it LEFT, can
	// compare against what it was told to write rather than against the
	// configuration it was told to override. Without them a run given a
	// --var-file would write exactly what was asked and then report drift for
	// doing so (FR-044).
	//
	// `check` never sets this: it has no --var-file, and comparing against the
	// declared configuration is precisely its job.
	Overrides map[string]string
	// Now is the clock a token's remaining lifetime is measured against. A zero
	// Now uses time.Now, which is what production does.
	Now time.Time
}

// Evaluate runs the whole catalog and returns the report (FR-021).
//
// The catalog is: the default-branch check, the branch-protection check, the
// protected-tags check, and one check per selected variable. With no profile
// selected only the first two run, and the caller is expected to warn that CI
// variables were not checked (FR-019).
//
// A platform failure aborts with an error, because a check that could not be
// run is not the same as a check that failed: the first is exit 1, the second
// exit 3 (CLI-002).
func (e *Evaluator) Evaluate(ctx context.Context) (*Report, error) {
	report := &Report{
		Command:    "check",
		Repository: e.Repository,
		Instance:   e.Instance,
		Profiles:   e.Selection.Names,
	}

	branch := e.Config.Settings.DefaultBranch

	branchResult, err := EvaluateBranch(ctx, e.Forge, branch)
	if err != nil {
		return nil, err
	}
	report.Checks = append(report.Checks, branchResult)

	protectionResult, err := EvaluateProtection(
		ctx, e.Forge, branch, e.Config.BranchProtection, e.Instance.Platform)
	if err != nil {
		return nil, err
	}
	report.Checks = append(report.Checks, protectionResult)

	tagResults, err := EvaluateTags(ctx, e.Forge, e.Selection.ProtectedTags)
	if err != nil {
		return nil, err
	}
	report.Checks = append(report.Checks, tagResults...)

	varResults, err := e.evaluateVariables(ctx)
	if err != nil {
		return nil, err
	}
	report.Checks = append(report.Checks, varResults...)

	report.Summarise()

	return report, nil
}

// evaluateVariables runs one check per selected variable (FR-021).
//
// With no profile selected there are no variables, so only the branch and
// protection checks run and the caller warns that CI variables were not
// examined (FR-019).
func (e *Evaluator) evaluateVariables(ctx context.Context) ([]CheckResult, error) {
	results := make([]CheckResult, 0, len(e.Selection.Variables))

	for _, v := range e.Selection.Variables {
		var (
			result CheckResult
			err    error
		)

		// A generated variable takes the token path, which is the only one that
		// consults the project access token lifecycle.
		if v.Generator != nil {
			result, err = EvaluateGenerated(ctx, e.Forge, v, e.clock())
		} else {
			result, err = EvaluateVariable(ctx, e.Forge, v, e.expected(v), e.Instance.Platform)
		}
		if err != nil {
			return nil, err
		}

		results = append(results, result)
	}

	return results, nil
}

// clock returns the evaluator's notion of now.
func (e *Evaluator) clock() time.Time {
	if e.Now.IsZero() {
		return time.Now()
	}

	return e.Now
}

// expected resolves what a variable is supposed to hold, following the same
// precedence apply writes with: a per-run override first, then the declaration.
//
// A generated variable is never known here: its value comes from the platform,
// so there is nothing to compare it against.
func (e *Evaluator) expected(v config.VariableDefinition) Expected {
	if override, overridden := e.Overrides[v.Name]; overridden {
		return Expected{Value: override, Known: true}
	}

	switch v.Source() {
	case config.SourceLiteral:
		return Expected{Value: v.Value, Known: true}

	case config.SourceRef:
		value, declared := e.Config.Values[v.ValueRef]

		return Expected{Value: value, Known: declared}

	case config.SourceGenerator:
		return Expected{}

	default:
		return Expected{}
	}
}
