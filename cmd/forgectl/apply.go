package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/sgaunet/forgectl/internal/apply"
	"github.com/sgaunet/forgectl/internal/compliance"
	"github.com/sgaunet/forgectl/internal/config"
	"github.com/sgaunet/forgectl/internal/forge"
	"github.com/sgaunet/forgectl/internal/report"
	"github.com/sgaunet/forgectl/internal/values"
)

// errCancelled reports that the maintainer declined the confirmation. It is a
// successful outcome, not a failure: nothing was modified and that is what they
// asked for.
var errCancelled = errors.New("cancelled")

// errPartial reports a run that completed only in part. The repository is in
// neither the state it was in nor the one that was asked for, which CLI-002
// classifies as a runtime failure: exit 1, not 3.
var errPartial = errors.New("apply completed only in part; rerun to converge from here")

// applyFlags are the options that belong to apply alone.
type applyFlags struct {
	yes             bool
	deleteOldBranch bool
	varFile         string
	only            string
	skip            string
	forceRotate     bool
}

// newApplyCommand builds `forgectl apply`.
func newApplyCommand(opts *options) *cobra.Command {
	flags := &applyFlags{}

	cmd := &cobra.Command{
		Use:   "apply [PROFILES...]",
		Short: "Apply the fixes that bring the repository into line",
		Long: `apply prints the actions it intends to take, asks for confirmation, then
performs them in a fixed order: default branch, then protection including tags,
then variables.

It is idempotent: on an already-compliant repository the plan is empty, no
confirmation is asked for, and no state-changing call is made.

The plan preview and the confirmation prompt go to stderr, so
` + "`forgectl apply --yes --output=json | jq`" + ` yields a clean document.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runApply(cmd, opts, flags, args)
		},
	}

	cmd.Flags().BoolVarP(&flags.yes, "yes", "y", false, "skip the confirmation prompt")
	cmd.Flags().BoolVar(&flags.deleteOldBranch, "delete-old-branch", false,
		"delete the old remote branch once the default branch has been switched")
	cmd.Flags().StringVar(&flags.varFile, "var-file", "",
		"YAML file overriding variable values, one-off and per-repository")
	cmd.Flags().StringVar(&flags.only, "only", "",
		"restrict to these domains: branch, protection, vars (comma-separated)")
	cmd.Flags().StringVar(&flags.skip, "skip", "",
		"exclude these domains: branch, protection, vars (comma-separated)")
	cmd.Flags().BoolVar(&flags.forceRotate, "force-rotate", false,
		"rotate generated tokens even when no drift was found")

	return cmd
}

// runApply is the command body: evaluate, plan, confirm, execute, report.
func runApply(cmd *cobra.Command, opts *options, flags *applyFlags, args []string) error {
	ctx := cmd.Context()

	only, skip, err := flags.domains()
	if err != nil {
		return err
	}

	session, err := opts.begin(ctx, cmd, args)
	if err != nil {
		return err
	}

	// The value-resolution chain is built BEFORE the forge is opened, because
	// building it performs the refusals of FR-056 and FR-057: a values file
	// that lies inside the working copy while git does not ignore it must be
	// refused before any platform call and before any value is read from it.
	// Opening the forge first would turn that refusal into a network error.
	resolver, err := session.resolver(ctx, flags.varFile, opts.stderr)
	if err != nil {
		return err
	}

	client, err := session.openForge()
	if err != nil {
		return err
	}

	writer, ok := client.(forge.Forge)
	if !ok {
		return fmt.Errorf("%w: the %s client cannot apply changes",
			errUsage, session.target.Instance.Platform)
	}

	evaluated, err := session.evaluator(client).Evaluate(ctx)
	if err != nil {
		return err
	}
	evaluated.Command = "apply"

	if session.selection.Empty() {
		opts.warnf(evaluated, "no profile selected, so CI variables were not checked")
	}

	plan, err := session.buildPlan(ctx, evaluated, client, flags)
	if err != nil {
		return err
	}
	plan = plan.Filter(only, skip)

	// An empty plan means a compliant repository: no confirmation is asked for
	// and no mutating call is made (FR-035).
	if plan.Empty() {
		fmt.Fprintln(opts.stderr, "nothing to do: the repository already matches the conventions")

		return opts.render(evaluated, session.resolved.Output)
	}

	if err := opts.confirm(plan, flags.yes); err != nil {
		// Declining is a successful outcome, not a failure: nothing was
		// modified, which is what was asked for. The drift is still there, so
		// the run reports it the way check would (US2 acceptance scenario 6).
		if errors.Is(err, errCancelled) {
			return opts.render(evaluated, session.resolved.Output)
		}

		return err
	}

	// Every value the run will write is resolved to completion BEFORE the first
	// write, so a value that turns out to be missing cannot leave the
	// repository half converged (FR-044).
	if err := resolver.CheckComplete(ctx, plan.VariableTargets()); err != nil {
		return err //nolint:wrapcheck // the message already lists every missing name
	}

	executor := &apply.Executor{
		Forge:       writer,
		WorkCopy:    session.workCopy,
		Config:      &session.resolved.Config,
		Values:      resolver,
		Definitions: session.definitions(),
		Warn:        func(format string, args ...any) { opts.warnf(evaluated, format, args...) },
	}

	result, err := executor.Run(ctx, plan)
	if err != nil {
		return err //nolint:wrapcheck // already described by the executor
	}

	// The report must describe the state apply LEFT, not the one it found:
	// re-evaluating is the only way to know what remains, which is what the
	// exit code has to reflect (CLI-002). It also means a successful run
	// reports the compliant repository it produced rather than the drift it
	// started from.
	// The re-evaluation compares against what apply was TOLD to write, which
	// with a --var-file is not what the configuration declares. Comparing
	// against the configuration would report drift for doing exactly what was
	// asked (FR-044).
	evaluator := session.evaluator(client)
	evaluator.Overrides = resolver.Overrides()

	final, err := evaluator.Evaluate(ctx)
	if err != nil {
		return err
	}

	final.Command = "apply"
	final.Actions = result.Actions
	final.Warnings = evaluated.Warnings

	return opts.finish(final, session.resolved.Output, result)
}

// resolver builds the value-resolution chain of FR-044: the override file, the
// configuration, and — only when a terminal is attached — a concealed prompt.
func (s *session) resolver(ctx context.Context, varFile string, stderr io.Writer) (*values.Resolver, error) {
	file, err := values.LoadVarFile(ctx, varFile, s.workCopy.Root)
	if err != nil {
		return nil, err //nolint:wrapcheck // the sentinel must stay reachable for classify
	}

	return values.NewResolver(
		s.selection,
		s.resolved.Config.Values,
		file.Values,
		values.NewTerminalPrompter(stderr),
	), nil
}

// definitions keys the selected variables by name, for the executor.
func (s *session) definitions() map[string]config.VariableDefinition {
	out := make(map[string]config.VariableDefinition, len(s.selection.Variables))
	for _, v := range s.selection.Variables {
		out[v.Name] = v
	}

	return out
}

// domains parses --only and --skip, which cannot be combined (FR-036).
func (f *applyFlags) domains() ([]compliance.Domain, []compliance.Domain, error) {
	if f.only != "" && f.skip != "" {
		return nil, nil, fmt.Errorf("%w: --only and --skip cannot be combined", errUsage)
	}

	only, err := compliance.ParseDomains(f.only)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: --only: %w", errUsage, err)
	}

	skip, err := compliance.ParseDomains(f.skip)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: --skip: %w", errUsage, err)
	}

	return only, skip, nil
}

// buildPlan gathers the local facts plan construction needs and builds the plan.
func (s *session) buildPlan(
	ctx context.Context, evaluated *compliance.Report, client forge.Reader, flags *applyFlags,
) (compliance.Plan, error) {
	want := s.resolved.Config.Settings.DefaultBranch

	actual, err := client.DefaultBranch(ctx)
	if err != nil {
		return compliance.Plan{}, fmt.Errorf("reading the default branch: %w", err)
	}

	remotely, err := client.BranchExists(ctx, want)
	if err != nil {
		return compliance.Plan{}, fmt.Errorf("checking whether %s exists: %w", want, err)
	}

	locally, err := s.workCopy.LocalBranchExists(ctx, want)
	if err != nil {
		return compliance.Plan{}, fmt.Errorf("checking the local branches: %w", err)
	}

	return compliance.BuildPlan(evaluated, compliance.PlanInput{
		WantBranch:              want,
		ActualBranch:            actual,
		NewBranchExistsRemotely: remotely,
		NewBranchExistsLocally:  locally,
		DeleteOldBranch:         flags.deleteOldBranch,
		Protection:              s.resolved.Config.BranchProtection,
		ForceRotate:             flags.forceRotate,
	}), nil
}

// confirm shows the plan and asks for consent, on stderr.
//
// Writing the preview and the prompt to stdout would corrupt the document a
// caller is piping into jq, which is why both live on stderr (CLI-001).
func (o *options) confirm(plan compliance.Plan, yes bool) error {
	doc := make([]report.Action, 0, len(plan.Actions))
	for _, action := range plan.Actions {
		doc = append(doc, report.Action{
			Domain:      action.Domain.String(),
			Description: action.Description,
			Destructive: action.Destructive,
			Status:      "planned",
		})
	}

	if err := report.WritePlan(o.stderr, doc); err != nil {
		return err //nolint:wrapcheck // already described by the renderer
	}

	if yes {
		return nil
	}

	if !o.stdinIsTerminal() {
		return fmt.Errorf(
			"%w: apply needs confirmation, but stdin is not a terminal; pass --yes to proceed",
			errUsage)
	}

	fmt.Fprint(o.stderr, "Confirm? [y/N] ")

	var answer string
	_, _ = fmt.Fscanln(os.Stdin, &answer)

	if answer != "y" && answer != "Y" && answer != "yes" {
		fmt.Fprintln(o.stderr, "cancelled: nothing was modified")

		return errCancelled
	}

	return nil
}

// stdinIsTerminal reports whether a human is there to answer the prompt.
func (o *options) stdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// finish renders the applied-action report and derives the outcome.
//
// A run that completed only in part is a runtime failure, exit 1, even though
// some actions succeeded: the repository is in neither the state it was in nor
// the one that was asked for (CLI-002).
func (o *options) finish(r *compliance.Report, format string, result apply.Result) error {
	renderErr := o.render(r, format)

	if result.Failed() {
		return errPartial
	}

	return renderErr
}
