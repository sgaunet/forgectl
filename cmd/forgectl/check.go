package main

import (
	"github.com/spf13/cobra"
)

// newCheckCommand builds `forgectl check`, the read-only audit.
//
// The command constructs an evaluator and nothing else. It cannot reach
// internal/apply, which is not merely a convention here: internal/compliance
// does not import it either, so FR-031's "MUST NOT modify any state" is a
// property of the import graph.
func newCheckCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "check [PROFILES...]",
		Short: "Compare the repository against the conventions (read-only)",
		Long: `check compares the repository against the declared conventions and reports
what drifted. It modifies nothing, on the platform or locally.

PROFILES are profile names, cumulative and deduplicated. When none is given,
the profiles listed in .forgectl.yaml at the repository root are used; when
neither supplies any, only the branch and protection checks run.

Exits 0 when the repository is compliant and 3 when drift remains.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			session, err := opts.begin(cmd.Context(), cmd, args)
			if err != nil {
				return err
			}

			client, err := session.openForge()
			if err != nil {
				return err
			}

			report, err := session.evaluator(client).Evaluate(cmd.Context())
			if err != nil {
				return err
			}

			// FR-019: with no profile selected, the CI variables were not
			// examined at all, and saying so is the difference between "clean"
			// and "not looked at".
			if session.selection.Empty() {
				opts.warnf(report, "no profile selected, so CI variables were not checked")
			}

			return opts.render(report, session.resolved.Output)
		},
	}
}
