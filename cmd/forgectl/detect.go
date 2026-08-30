package main

import (
	"github.com/spf13/cobra"
)

// newDetectCommand builds `forgectl detect`, which reports what forge and
// repository the working copy points at and changes nothing (FR-004).
func newDetectCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "detect",
		Short: "Print the detected forge and repository",
		Long: `detect reports the repository owner and name, the instance, the host, the
platform, and the API base URL.

It is read-only, makes no platform call, and never prompts.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			session, err := opts.begin(cmd.Context(), cmd, nil)
			if err != nil {
				return err
			}

			// Detection is answered entirely from the working copy and the
			// configuration, so it makes no platform call at all.
			return opts.render(session.newReport("detect"), session.resolved.Output)
		},
	}
}
