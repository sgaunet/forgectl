package report

import (
	"encoding/json"
	"fmt"
	"io"
)

// WriteJSON emits one document per run, to stdout and nowhere else (CLI-001).
//
// The plan preview, the confirmation prompt, warnings, and logs all go to
// stderr precisely so this document stays a clean, parseable whole:
// `forgectl apply --yes --output=json | jq` must yield one value.
func WriteJSON(w io.Writer, doc Document) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	// A value can contain anything, so HTML escaping would be a poor defence;
	// the real defence is that no value ever reaches this document. Turning
	// escaping off keeps the output readable for the maintainer.
	enc.SetEscapeHTML(false)

	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("writing the JSON report: %w", err)
	}

	return nil
}
