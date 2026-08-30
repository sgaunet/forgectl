package main

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// newVersionCommand builds `forgectl version`.
//
// The version comes from the module build information the toolchain embeds, not
// from ldflags: a binary built with `go install` then reports a real version
// rather than "dev", and goreleaser's build already stamps the same data
// (Constitution II).
func newVersionCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version and build information",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Fprintln(opts.stdout, buildInfo())

			return nil
		},
	}
}

// buildInfo renders the version, revision, and build time.
func buildInfo() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "forgectl (version unknown: no build information embedded)"
	}

	version := info.Main.Version
	if version == "" || version == "(devel)" {
		version = "devel"
	}

	var revision, modified, built string
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value
		case "vcs.time":
			built = setting.Value
		}
	}

	out := "forgectl " + version
	if revision != "" {
		out += " (" + shortRevision(revision)
		if modified == "true" {
			out += "-dirty"
		}
		if built != "" {
			out += ", built " + built
		}
		out += ")"
	}

	return out
}

// shortRevision abbreviates a commit hash the way git does.
func shortRevision(revision string) string {
	const short = 12

	if len(revision) <= short {
		return revision
	}

	return revision[:short]
}
