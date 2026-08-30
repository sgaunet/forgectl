package main

import "github.com/spf13/cobra"

// subcommands builds the command tree. It is the one place a command is
// registered, so the root command stays about flags and streams rather than
// about the catalogue.
func subcommands(opts *options) []*cobra.Command {
	return []*cobra.Command{
		newDetectCommand(opts),
		newCheckCommand(opts),
		newApplyCommand(opts),
		newProfilesCommand(opts),
		newVersionCommand(opts),
	}
}
