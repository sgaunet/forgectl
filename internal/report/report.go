package report

import (
	"time"

	"github.com/sgaunet/forgectl/internal/compliance"
	"github.com/sgaunet/forgectl/internal/config"
)

// Document is the machine-readable shape of one run, conforming to
// contracts/output.schema.json. It is identical for check and apply; apply
// additionally populates Actions.
//
// No field of this document ever carries a configured or generated value
// (FR-054). The types below simply have nowhere to put one.
type Document struct {
	Command    string   `json:"command"`
	Repository string   `json:"repository"`
	Instance   Instance `json:"instance"`
	Profiles   []string `json:"profiles"`
	Checks     []Check  `json:"checks"`
	Actions    []Action `json:"actions,omitempty"`
	Summary    Summary  `json:"summary"`
	Warnings   []string `json:"warnings,omitempty"`
}

// Instance identifies the forge a run targeted. The name of the credential
// environment variable is deliberately absent: what the run used to
// authenticate is not part of its result.
type Instance struct {
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Host     string `json:"host"`
	APIURL   string `json:"api_url,omitempty"`
}

// Check is one catalog entry's outcome. The generated-variable fields are flat
// siblings rather than a nested object, as the schema declares.
type Check struct {
	ID       string `json:"id"`
	Domain   string `json:"domain"`
	Status   string `json:"status"`
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Fixable  *bool  `json:"fixable,omitempty"`
	// Pattern is set on the tags check: the tag pattern evaluated.
	Pattern string `json:"pattern,omitempty"`
	// Generator names the generator kind, set only for a generated variable.
	Generator string `json:"generator,omitempty"`
	// ExpiresAt is the token's expiry date, set only when a token is active.
	ExpiresAt string `json:"expires_at,omitempty"`
	// RotateInDays is the days remaining before the rotation threshold, and is
	// negative once it is past.
	RotateInDays *int `json:"rotate_in_days,omitempty"`
}

// Action is one step apply took, in execution order.
type Action struct {
	Domain      string `json:"domain"`
	Description string `json:"description"`
	Destructive bool   `json:"destructive,omitempty"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
}

// Summary counts the outcomes of a run.
type Summary struct {
	Pass int `json:"pass"`
	Fail int `json:"fail"`
	Skip int `json:"skip"`
}

// FromReport projects an evaluated report onto the output document.
//
// The projection is deliberately total and explicit: every field it copies is a
// status or a state description. There is no field on the source types that
// could carry a value, so no value can reach the document by omission.
func FromReport(r *compliance.Report) Document {
	doc := Document{
		Command:    r.Command,
		Repository: r.Repository,
		Instance:   instanceOf(r.Instance),
		Profiles:   nonNilStrings(r.Profiles),
		Checks:     make([]Check, 0, len(r.Checks)),
		Summary:    Summary{Pass: r.Summary.Pass, Fail: r.Summary.Fail, Skip: r.Summary.Skip},
		Warnings:   r.Warnings,
	}

	for _, c := range r.Checks {
		doc.Checks = append(doc.Checks, checkOf(c))
	}

	for _, a := range r.Actions {
		doc.Actions = append(doc.Actions, Action{
			Domain:      a.Domain.String(),
			Description: a.Description,
			Destructive: a.Destructive,
			Status:      a.Status.String(),
			Error:       a.Error,
		})
	}

	return doc
}

// instanceOf projects the instance a run targeted.
func instanceOf(inst config.Instance) Instance {
	return Instance{
		Name:     inst.Name,
		Platform: inst.Platform.String(),
		Host:     inst.Host,
		APIURL:   inst.APIURL,
	}
}

// checkOf projects one check result.
func checkOf(c compliance.CheckResult) Check {
	out := Check{
		ID:       c.ID,
		Domain:   c.Domain.String(),
		Status:   c.Status.String(),
		Expected: c.Expected,
		Actual:   c.Actual,
		Reason:   c.Reason,
		Pattern:  c.Pattern,
	}

	// fixable is meaningful only on a failure: a passing check has nothing to
	// fix, and a skipped one was never evaluated.
	if c.Status == compliance.StatusFail {
		fixable := c.Fixable
		out.Fixable = &fixable
	}

	if c.Generator != nil {
		out.Generator = c.Generator.Kind

		// The expiry and the days remaining exist only when there is a token to
		// measure. A missing or ambiguous token has neither, and emitting a
		// zero would read as "expires today".
		if !c.Generator.ExpiresAt.IsZero() {
			out.ExpiresAt = formatDate(c.Generator.ExpiresAt)
			days := c.Generator.RotateInDays
			out.RotateInDays = &days
		}
	}

	return out
}

// formatDate renders an expiry as the calendar date the platform speaks in,
// and an unset one as the empty string.
func formatDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return t.UTC().Format(time.DateOnly)
}

// nonNilStrings makes an absent list render as [] rather than null, which the
// schema requires of profiles.
func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}

	return in
}
