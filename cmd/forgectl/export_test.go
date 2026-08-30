package main

import "io"

// This file exposes the internals a package main_test needs, so the tests stay
// black box without the classifier having to become public API
// (Constitution VII).

// Classify exposes the exit-code classifier to the test package.
func Classify(err error) int { return classify(err) }

// Run exposes main's body, so a test can exercise the whole command tree with
// its own streams and arguments.
func Run(args []string, stdout, stderr io.Writer) int { return run(args, stdout, stderr) }

// The sentinels a test needs to build the errors classify must recognise.
var (
	ErrDrift = errDrift
	ErrUsage = errUsage
)

// The exit codes, so a test names them rather than repeating the numbers.
const (
	ExitOK      = exitOK
	ExitRuntime = exitRuntime
	ExitUsage   = exitUsage
	ExitDrift   = exitDrift
)
