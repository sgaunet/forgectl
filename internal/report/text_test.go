package report_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sgaunet/forgectl/internal/report"
)

// update rewrites the golden files instead of comparing against them.
var update = flag.Bool("update", false, "rewrite the golden files")

// golden compares rendered output against testdata/<name>.txt.
func golden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", name+".txt")

	if *update {
		if err := os.MkdirAll("testdata", 0o750); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}

		return
	}

	want, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("reading %s (run `go test ./internal/report -update` to create it): %v", path, err)
	}

	if got != string(want) {
		t.Errorf("rendered output differs from %s.\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

func TestTextRenderMatchesTheGoldenFile(t *testing.T) {
	var buf bytes.Buffer
	if err := report.WriteText(&buf, report.FromReport(fullReport()), report.Palette{}); err != nil {
		t.Fatalf("WriteText: %v", err)
	}

	golden(t, "apply", buf.String())
}

func TestTextRenderEmitsNoEscapeSequencesWithoutAPalette(t *testing.T) {
	var buf bytes.Buffer
	if err := report.WriteText(&buf, report.FromReport(fullReport()), report.Palette{}); err != nil {
		t.Fatalf("WriteText: %v", err)
	}

	// Constitution V: no colour when stdout is not a TTY, and none when
	// NO_COLOR is set. The renderer is given an empty palette in both cases.
	if strings.Contains(buf.String(), "\x1b[") {
		t.Error("the renderer emitted an ANSI escape sequence with no palette")
	}
}

func TestTextRenderColoursWhenGivenAPalette(t *testing.T) {
	var buf bytes.Buffer
	if err := report.WriteText(&buf, report.FromReport(fullReport()), report.ColourPalette()); err != nil {
		t.Fatalf("WriteText: %v", err)
	}

	if !strings.Contains(buf.String(), "\x1b[") {
		t.Error("the renderer emitted no colour despite being given a palette")
	}
}

func TestTextRenderNamesTheDetectionFacts(t *testing.T) {
	// FR-004: detect reports the owner and name, the instance, the host, the
	// platform, and the API base URL.
	var buf bytes.Buffer
	if err := report.WriteText(&buf, report.FromReport(fullReport()), report.Palette{}); err != nil {
		t.Fatalf("WriteText: %v", err)
	}

	for _, want := range []string{
		"acme/my-tool", "gitlab.com", "gitlab", "https://gitlab.com/api/v4",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("the render omits %q", want)
		}
	}
}

func TestPlanPreviewMarksDestructiveActions(t *testing.T) {
	doc := report.FromReport(fullReport())

	var buf bytes.Buffer
	if err := report.WritePlan(&buf, doc.Actions); err != nil {
		t.Fatalf("WritePlan: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "forgectl will:") {
		t.Error("the plan preview does not announce itself")
	}
	// CLI-003: a destructive action is visibly marked, because that is what the
	// maintainer is being asked to confirm.
	if !strings.Contains(out, "! ") {
		t.Error("the plan preview does not mark its destructive action")
	}
}

func TestNoValueReachesTheTextRender(t *testing.T) {
	// SC-003, from the renderer's side: the document carries only statuses and
	// state descriptions, so a value has no field to travel in.
	var buf bytes.Buffer
	if err := report.WriteText(&buf, report.FromReport(fullReport()), report.Palette{}); err != nil {
		t.Fatalf("WriteText: %v", err)
	}

	for _, sentinel := range []string{"glpat-", "ssh-rsa", "BEGIN OPENSSH PRIVATE KEY"} {
		if strings.Contains(buf.String(), sentinel) {
			t.Errorf("the render contains %q", sentinel)
		}
	}
}
