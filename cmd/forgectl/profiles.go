package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sgaunet/forgectl/internal/config"
	"github.com/sgaunet/forgectl/internal/report"
)

// newProfilesCommand builds `forgectl profiles`, the discovery commands.
//
// They are read-only, make no platform call, and never prompt. They also work
// with no configuration file at all and outside a git working copy: someone
// deciding whether to adopt a profile has no reason to be standing in a
// repository first (SC-009).
func newProfilesCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profiles",
		Short: "List the available profiles, or show one in detail",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help() //nolint:wrapcheck // cobra's own help error, unchanged
		},
	}

	cmd.AddCommand(newProfilesListCommand(opts), newProfilesShowCommand(opts))

	return cmd
}

// newProfilesListCommand builds `forgectl profiles list`.
func newProfilesListCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every available profile, built-in and configured",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolved, err := opts.loadConfig(cmd)
			if err != nil {
				return err
			}

			listing := report.ListingOf(&resolved.Config)

			if resolved.Output == "json" {
				return writeJSON(opts, listing)
			}

			return report.WriteListingText(opts.stdout, listing)
		},
	}
}

// newProfilesShowCommand builds `forgectl profiles show TYPE`.
func newProfilesShowCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "show TYPE",
		Short: "Show one profile: its variables, their attributes, and where each value comes from",
		Long: `show displays each variable's name, attributes, and value-source kind, plus
the profile's protected tag patterns.

It shows no value, and has nowhere to put one.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := opts.loadConfig(cmd)
			if err != nil {
				return err
			}

			profile, err := config.LookupProfile(&resolved.Config, args[0])
			if err != nil {
				return err //nolint:wrapcheck // the sentinel must stay reachable for classify
			}

			detail := report.DetailOf(&resolved.Config, profile, args[0])

			if resolved.Output == "json" {
				return writeJSON(opts, detail)
			}

			return report.WriteDetailText(opts.stdout, detail)
		},
	}
}

// writeJSON emits one document to stdout, in the same shape the other commands
// use: indented, and the only thing on that stream.
func writeJSON(opts *options, doc any) error {
	enc := json.NewEncoder(opts.stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)

	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("writing the JSON document: %w", err)
	}

	return nil
}
