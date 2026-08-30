package values

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
)

// TerminalPrompter asks for a value on the terminal, with the input concealed.
//
// The prompt is written to stderr, never to stdout: stdout carries the run's
// data, and a prompt there would corrupt a document being piped into jq
// (CLI-001).
type TerminalPrompter struct {
	// Out is where the prompt text goes. It must be stderr.
	Out io.Writer
}

// NewTerminalPrompter returns a prompter when a terminal is attached, and nil
// when there is none.
//
// Returning nil rather than a prompter that fails is deliberate: the resolver
// then reports "no value, and no terminal to ask" as one clear condition,
// instead of hanging on a read that will never be answered (FR-044, CLI-003).
func NewTerminalPrompter(out io.Writer) Prompter {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return nil
	}

	return &TerminalPrompter{Out: out}
}

// Prompt asks for one value without echoing it.
func (p *TerminalPrompter) Prompt(name string) (string, error) {
	fmt.Fprintf(p.Out, "Value for %s (input hidden): ", name)

	// ReadPassword puts the terminal into no-echo mode through termios, which
	// is the only way to read without the value appearing on screen — and, on a
	// shared terminal, in someone else's scrollback (R10).
	raw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(p.Out)

	if err != nil {
		return "", fmt.Errorf("reading the concealed input: %w", err)
	}

	return string(raw), nil
}
