package report

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

// Palette carries the escape sequences the renderer uses. A zero Palette emits
// none, which is what happens whenever NO_COLOR is set or stdout is not a
// terminal (CLI-004, Constitution V).
type Palette struct {
	Pass  string
	Fail  string
	Skip  string
	Dim   string
	Reset string
}

// ColourPalette is used only when stdout is a terminal and NO_COLOR is unset.
func ColourPalette() Palette {
	return Palette{
		Pass:  "\x1b[32m",
		Fail:  "\x1b[31m",
		Skip:  "\x1b[33m",
		Dim:   "\x1b[2m",
		Reset: "\x1b[0m",
	}
}

// WriteText renders the report for a human reader, to stdout and nowhere else.
func WriteText(w io.Writer, doc Document, p Palette) error {
	var b strings.Builder

	writeHeader(&b, doc)
	writeChecks(&b, doc, p)
	writeActions(&b, doc, p)
	writeSummary(&b, doc, p)

	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("writing the report: %w", err)
	}

	return nil
}

// writeHeader states what was inspected, which is the whole of `detect` and the
// first lines of every other command.
func writeHeader(b *strings.Builder, doc Document) {
	tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)

	fmt.Fprintf(tw, "Repository\t%s\n", doc.Repository)
	fmt.Fprintf(tw, "Instance\t%s\n", doc.Instance.Name)
	fmt.Fprintf(tw, "Host\t%s\n", doc.Instance.Host)
	fmt.Fprintf(tw, "Platform\t%s\n", doc.Instance.Platform)

	if doc.Instance.APIURL != "" {
		fmt.Fprintf(tw, "API\t%s\n", doc.Instance.APIURL)
	}
	if len(doc.Profiles) > 0 {
		fmt.Fprintf(tw, "Profiles\t%s\n", strings.Join(doc.Profiles, ", "))
	}

	_ = tw.Flush()
}

// writeChecks renders the compliance table.
func writeChecks(b *strings.Builder, doc Document, p Palette) {
	if len(doc.Checks) == 0 {
		return
	}

	b.WriteString("\n")
	tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)

	for _, c := range doc.Checks {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", statusMark(c.Status, p), c.ID, checkDetail(c))
	}

	_ = tw.Flush()
}

// checkDetail is the one-line explanation beside a check. It renders only
// states — "expected main, found master", "missing", "expires in 12 days" —
// because that is all the check result can carry (FR-054).
func checkDetail(c Check) string {
	switch c.Status {
	case "fail":
		detail := failDetail(c)
		if c.Fixable != nil && !*c.Fixable {
			detail += " (not auto-fixable)"
		}

		return detail
	case "skip":
		return "skipped: " + c.Reason
	default:
		return passDetail(c)
	}
}

// failDetail renders a failure's expected and actual state.
func failDetail(c Check) string {
	switch {
	case c.Expected != "" && c.Actual != "":
		return fmt.Sprintf("expected %s, found %s", c.Expected, c.Actual)
	case c.Actual != "":
		return c.Actual
	case c.Expected != "":
		return "expected " + c.Expected
	default:
		return "failed"
	}
}

// passDetail adds the remaining token lifetime to a passing generated variable,
// which is the one fact a maintainer wants without asking.
func passDetail(c Check) string {
	if c.Generator != "" && c.RotateInDays != nil {
		return fmt.Sprintf("ok, rotates in %d days", *c.RotateInDays)
	}

	return "ok"
}

// writeActions renders what apply did, in execution order.
func writeActions(b *strings.Builder, doc Document, p Palette) {
	if len(doc.Actions) == 0 {
		return
	}

	b.WriteString("\nActions\n")
	tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)

	for _, a := range doc.Actions {
		line := a.Description
		if a.Error != "" {
			line += ": " + a.Error
		}
		fmt.Fprintf(tw, "%s\t%s\n", actionMark(a.Status, p), line)
	}

	_ = tw.Flush()
}

// writeSummary closes with the counts a reader scans for first.
func writeSummary(b *strings.Builder, doc Document, p Palette) {
	fmt.Fprintf(b, "\n%d passed, %d failed, %d skipped\n",
		doc.Summary.Pass, doc.Summary.Fail, doc.Summary.Skip)

	if doc.Summary.Fail > 0 {
		fmt.Fprintf(b, "%sdrift found%s\n", p.Fail, p.Reset)
	}
}

// statusMark renders a check's status, coloured only when a palette was given.
func statusMark(status string, p Palette) string {
	switch status {
	case "pass":
		return p.Pass + "PASS" + p.Reset
	case "fail":
		return p.Fail + "FAIL" + p.Reset
	default:
		return p.Skip + "SKIP" + p.Reset
	}
}

// actionMark renders an executed action's status.
func actionMark(status string, p Palette) string {
	switch status {
	case "done":
		return p.Pass + "done" + p.Reset
	case "failed":
		return p.Fail + "failed" + p.Reset
	default:
		return p.Dim + status + p.Reset
	}
}

// WritePlan renders the plan preview. It goes to STDERR, never to stdout: a
// prompt or a preview written to stdout would corrupt the document a caller is
// piping into jq (CLI-001).
func WritePlan(w io.Writer, actions []Action) error {
	var b strings.Builder

	b.WriteString("forgectl will:\n")
	for _, a := range actions {
		mark := "  -"
		if a.Destructive {
			mark = "  ! "
		}
		fmt.Fprintf(&b, "%s %s\n", mark, a.Description)
	}

	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("writing the plan preview: %w", err)
	}

	return nil
}

// WriteProfileList renders the available profiles, one per line, sorted.
func WriteProfileList(w io.Writer, names []string) error {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)

	for _, name := range sorted {
		if _, err := fmt.Fprintln(w, name); err != nil {
			return fmt.Errorf("writing the profile listing: %w", err)
		}
	}

	return nil
}
