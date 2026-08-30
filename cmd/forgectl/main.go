// Command forgectl checks a repository against declared forge conventions and
// converges it.
//
// This package is a thin wrapper: it parses, validates, calls, and formats.
// Every decision lives in a package under internal/ that imports no CLI code
// (Constitution III).
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/sgaunet/forgectl/internal/compliance"
	"github.com/sgaunet/forgectl/internal/config"
	"github.com/sgaunet/forgectl/internal/gitrepo"
	"github.com/sgaunet/forgectl/internal/report"
)

// The four exit codes, documented in --help and covered by tests (CLI-002).
//
// Cobra sets no exit code of its own — Execute returns an error and the
// scaffolded main exits 1 for everything — so all four are wired here
// explicitly (Constitution V).
const (
	exitOK      = 0 // succeeded; the repository is compliant
	exitRuntime = 1 // work began and something broke
	exitUsage   = 2 // the invocation or configuration was wrong; nothing was attempted
	exitDrift   = 3 // succeeded, drift remains
)

var (
	// errDrift is returned by a command that completed successfully while
	// leaving drift. It is a result, not a failure: it carries no message,
	// because the report already said everything.
	//
	// A command that hits a runtime failure returns THAT error instead of this
	// one, never both, which is what implements "the lowest non-zero code that
	// is not 0 wins": a runtime failure during a drifted check exits 1, not 3.
	errDrift = errors.New("drift found")

	// errUsage marks an invocation forgectl refused before attempting anything.
	// Cobra's own flag and argument errors are wrapped in it.
	errUsage = errors.New("usage error")
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main's testable body: it takes its streams and arguments rather than
// reaching for the globals, so the classifier and the stream split can both be
// exercised without building a binary.
func run(args []string, stdout, stderr io.Writer) int {
	// SIGINT and SIGTERM cancel the root context, which every platform call and
	// every git command carries. An interrupted apply stops at the current step
	// and reports what completed (CLI-005).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := newRootCommand(stdout, stderr)
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)

	err := root.ExecuteContext(ctx)
	code := classify(err)

	// Errors are printed here rather than by cobra, so the message always lands
	// on stderr and never mixes into the document on stdout (CLI-001).
	if err != nil && !errors.Is(err, errDrift) {
		fmt.Fprintf(stderr, "forgectl: %v\n", err)
	}

	return code
}

// classify maps an error onto a process exit code (R11).
//
// Exit 2 is the set of conditions where the invocation or the configuration was
// wrong and nothing was attempted; every one of them is a sentinel a domain
// package exports, so the mapping is a list rather than a guess. Everything
// else that failed is a runtime failure.
func classify(err error) int {
	switch {
	case err == nil:
		return exitOK
	case isUsage(err):
		return exitUsage
	case onlyDrift(err):
		return exitDrift
	default:
		return exitRuntime
	}
}

// onlyDrift reports whether drift is the ONLY thing wrong.
//
// CLI-002 fixes the tie-break: where more than one code applies, the lowest
// non-zero one wins, so a runtime failure during a drifted check exits 1 rather
// than 3. Checking that every leaf of the error tree is the drift sentinel
// enforces that here, rather than leaving it to the convention that a command
// returns one or the other.
func onlyDrift(err error) bool {
	// The assertions below inspect the SHAPE of the error tree — how it
	// unwraps — rather than matching error types, which is why they are not
	// errors.As calls.
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		leaves := joined.Unwrap()
		if len(leaves) == 0 {
			return false
		}

		for _, leaf := range leaves {
			if !onlyDrift(leaf) {
				return false
			}
		}

		return true
	}

	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return onlyDrift(wrapped.Unwrap())
	}

	return errors.Is(err, errDrift)
}

// isUsage reports whether the error is one of the conditions CLI-002 assigns to
// exit 2.
func isUsage(err error) bool {
	usage := []error{
		errUsage,
		config.ErrInvalid,
		config.ErrPermissions,
		config.ErrValuesInRepo,
		config.ErrUnknownProfile,
		config.ErrUnknownHost,
		config.ErrNoCredential,
		gitrepo.ErrNotARepo,
		gitrepo.ErrNoCommits,
		gitrepo.ErrNoRemote,
		gitrepo.ErrGitMissing,
		gitrepo.ErrRemoteURL,
	}

	for _, sentinel := range usage {
		if errors.Is(err, sentinel) {
			return true
		}
	}

	return false
}

// options carries the global flags, shared by every subcommand.
type options struct {
	configPath    string
	remote        string
	output        string
	json          bool
	noColor       bool
	allowInsecure bool
	verbose       bool
	quiet         bool

	stdout io.Writer
	stderr io.Writer
}

// newRootCommand builds the command tree and wires the global flags.
func newRootCommand(stdout, stderr io.Writer) *cobra.Command {
	opts := &options{stdout: stdout, stderr: stderr}

	root := &cobra.Command{
		Use:   "forgectl",
		Short: "Check a repository against forge conventions, and converge it",
		Long: `forgectl detects the forge hosting a repository's remote, checks the
repository against declared conventions — default branch, branch and tag
protection, and the CI variables a project-type profile declares — and applies
the fixes on request.

Nothing changes without an explicit apply, and no value is ever printed.

Streams
  stdout carries data only: the detection facts, the compliance report, the
  applied-action report, or the profile listing, selectable with --output.
  stderr carries logs, warnings, progress, errors, the plan preview, and the
  confirmation prompt.

Exit codes
  0  succeeded; the repository is compliant
  1  runtime failure — work began and something broke
  2  usage error — the invocation or configuration was wrong; nothing was tried
  3  succeeded, drift remains

Configuration precedence
  flags > environment > config file > defaults

Credentials are read only from the environment variable each instance names,
never from a flag or the configuration file.

forgectl runs the git binary for local work, so git must be on PATH.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		// A command that reaches its RunE has already parsed successfully, so a
		// bare `forgectl` should show help rather than fail.
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help() //nolint:wrapcheck // cobra's own help error, unchanged
		},
	}

	flags := root.PersistentFlags()
	flags.StringVar(&opts.configPath, "config", "", "configuration file (default ~/.config/forgectl/config.yaml)")
	flags.StringVar(&opts.remote, "remote", "origin", "remote to inspect")
	flags.StringVar(&opts.output, "output", "text", "output format: text or json")
	flags.BoolVar(&opts.json, "json", false, "alias for --output=json")
	flags.BoolVar(&opts.noColor, "no-color", false, "disable colour")
	flags.BoolVar(&opts.allowInsecure, "allow-insecure-config", false,
		"bypass the configuration file permission check, and nothing else")
	flags.BoolVarP(&opts.verbose, "verbose", "v", false, "verbose logs on stderr")
	flags.BoolVar(&opts.quiet, "quiet", false, "errors only on stderr")

	// Cobra's flag and argument errors are usage errors: nothing was attempted.
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return fmt.Errorf("%w: %w", errUsage, err)
	})

	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		return opts.prepare(cmd)
	}

	root.AddCommand(subcommands(opts)...)

	return root
}

// prepare validates the flag combination and installs the logger. It runs
// before every subcommand.
func (o *options) prepare(cmd *cobra.Command) error {
	if o.verbose && o.quiet {
		return fmt.Errorf("%w: --verbose and --quiet cannot be combined", errUsage)
	}

	if o.json {
		if cmd.Flags().Changed("output") && o.output != "json" {
			return fmt.Errorf("%w: --json and --output=%s cannot be combined", errUsage, o.output)
		}
		o.output = "json"
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(o.stderr, &slog.HandlerOptions{Level: o.logLevel()})))

	return nil
}

// logLevel maps the verbosity flags and FORGECTL_LOG_LEVEL onto a slog level.
// The flags win, per the precedence chain of CLI-004.
func (o *options) logLevel() slog.Level {
	switch {
	case o.quiet:
		return slog.LevelError
	case o.verbose:
		return slog.LevelDebug
	}

	switch os.Getenv("FORGECTL_LOG_LEVEL") {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "error":
		return slog.LevelError
	default:
		return slog.LevelWarn
	}
}

// loadConfig resolves the configuration through the four-layer merge, handing
// the merge what the flags actually set rather than what they hold, so an unset
// flag yields to the environment (R14).
func (o *options) loadConfig(cmd *cobra.Command) (*config.Resolved, error) {
	flags := cmd.Flags()

	env := config.ReadEnvironment()
	env.AllowInsecure = false

	resolved, err := config.Load(config.Options{
		Path:             o.configPath,
		PathSet:          flags.Changed("config"),
		Remote:           o.remote,
		RemoteSet:        flags.Changed("remote"),
		Output:           o.output,
		OutputSet:        flags.Changed("output") || o.json,
		AllowInsecure:    o.allowInsecure,
		AllowInsecureSet: flags.Changed("allow-insecure-config"),
	}, env)
	if err != nil {
		return nil, err //nolint:wrapcheck // the sentinel must stay reachable for classify
	}

	return resolved, nil
}

// palette returns the colours to render with: none whenever NO_COLOR is set,
// --no-color was given, or stdout is not a terminal (CLI-004, Constitution V).
func (o *options) palette() report.Palette {
	if o.noColor || os.Getenv("NO_COLOR") != "" || !o.stdoutIsTerminal() {
		return report.Palette{}
	}

	return report.ColourPalette()
}

// stdoutIsTerminal reports whether the data stream is a terminal.
func (o *options) stdoutIsTerminal() bool {
	file, ok := o.stdout.(*os.File)
	if !ok {
		return false
	}

	return term.IsTerminal(int(file.Fd()))
}

// render writes the report to stdout in the selected format, and returns
// errDrift when the run leaves drift, so the classifier can produce exit 3.
func (o *options) render(r *compliance.Report, format string) error {
	r.Summarise()

	doc := report.FromReport(r)

	var err error
	if format == "json" {
		err = report.WriteJSON(o.stdout, doc)
	} else {
		err = report.WriteText(o.stdout, doc, o.palette())
	}
	if err != nil {
		return err //nolint:wrapcheck // already described by the renderer
	}

	if r.Drifted() {
		return errDrift
	}

	return nil
}

// warn emits a non-fatal condition on stderr and records it in the report, so a
// JSON consumer sees it without reading two streams.
func (o *options) warnf(r *compliance.Report, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	r.Warnf("%s", msg)
	fmt.Fprintf(o.stderr, "warning: %s\n", msg)
}
