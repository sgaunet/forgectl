package compliance

import (
	"fmt"
	"strings"
	"time"

	"github.com/sgaunet/forgectl/internal/config"
)

// CheckStatus is the outcome of one check. Every check reports exactly one of
// these three (FR-022).
type CheckStatus string

// The three outcomes a check may report.
const (
	StatusPass CheckStatus = "pass"
	StatusFail CheckStatus = "fail"
	StatusSkip CheckStatus = "skip"
)

// String renders the status as it appears in output.
func (s CheckStatus) String() string { return string(s) }

// ParseCheckStatus converts a string into a CheckStatus, rejecting unknown
// values (R13).
func ParseCheckStatus(s string) (CheckStatus, error) {
	switch CheckStatus(s) {
	case StatusPass:
		return StatusPass, nil
	case StatusFail:
		return StatusFail, nil
	case StatusSkip:
		return StatusSkip, nil
	default:
		return "", fmt.Errorf("status %q is not one of pass, fail, skip", s)
	}
}

// Domain is the area of work a check and its action belong to. It is what
// --only and --skip filter on (FR-036).
type Domain string

// The three domains. The tags work belongs to protection, not to a fourth
// domain of its own (FR-036).
const (
	DomainBranch     Domain = "branch"
	DomainProtection Domain = "protection"
	DomainVars       Domain = "vars"
)

// String renders the domain as it appears in output and on the command line.
func (d Domain) String() string { return string(d) }

// ParseDomain converts a --only or --skip value into a Domain, rejecting
// unknown names (R13).
func ParseDomain(s string) (Domain, error) {
	switch Domain(strings.TrimSpace(s)) {
	case DomainBranch:
		return DomainBranch, nil
	case DomainProtection:
		return DomainProtection, nil
	case DomainVars:
		return DomainVars, nil
	default:
		return "", fmt.Errorf(
			"domain %q is not one of branch, protection, vars; the tags work belongs to protection", s)
	}
}

// ParseDomains parses a comma-separated domain list.
func ParseDomains(list string) ([]Domain, error) {
	if strings.TrimSpace(list) == "" {
		return nil, nil
	}

	parts := strings.Split(list, ",")
	out := make([]Domain, 0, len(parts))

	for _, part := range parts {
		d, err := ParseDomain(part)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}

	return out, nil
}

// The identifiers of the checks that do not belong to a variable (FR-021).
const (
	CheckBranch     = "branch"
	CheckProtection = "protection"
	CheckTags       = "tags"
	// VarPrefix prefixes a variable check's identifier: vars:<NAME>.
	VarPrefix = "vars:"
)

// VarCheckID builds the identifier of a variable's check.
func VarCheckID(name string) string { return VarPrefix + name }

// GeneratorStatus is the extra state a generated variable's check reports:
// when its token expires and how long is left (FR-055).
//
// It carries no value, and never could: the token's value exists only in the
// creation response and in the CI variable (FR-050).
type GeneratorStatus struct {
	Kind         string
	ExpiresAt    time.Time
	RotateInDays int
}

// CheckResult is one catalog entry's outcome.
//
// Expected, Actual, and Reason are message fields describing STATE. They must
// never contain a configured or generated value: only statuses such as
// "missing", "differs", or "expires in 12 days" (FR-054). The renderer receives
// nothing else, which is why a value cannot reach the output through this type.
type CheckResult struct {
	ID     string
	Domain Domain
	Status CheckStatus

	// Expected and Actual are set on a failure, and describe state.
	Expected string
	Actual   string
	// Reason is set on a skip, and says why the check could not be evaluated.
	Reason string
	// Fixable is false where apply cannot resolve the drift, such as a default
	// branch that is neither conventional name (FR-039).
	Fixable bool
	// Pattern is set on the tags check, naming the tag pattern evaluated.
	Pattern string

	// Generator is set only for a generated variable.
	Generator *GeneratorStatus
}

// Pass builds a passing result.
func Pass(id string, domain Domain) CheckResult {
	return CheckResult{ID: id, Domain: domain, Status: StatusPass, Fixable: true}
}

// Fail builds a failing result carrying the expected and actual state.
func Fail(id string, domain Domain, expected, actual string) CheckResult {
	return CheckResult{
		ID: id, Domain: domain, Status: StatusFail,
		Expected: expected, Actual: actual, Fixable: true,
	}
}

// Skip builds a skipped result carrying its reason. A skip is never drift: a
// run whose checks are all pass or skip is compliant.
func Skip(id string, domain Domain, reason string) CheckResult {
	return CheckResult{ID: id, Domain: domain, Status: StatusSkip, Reason: reason}
}

// NotFixable marks a failure apply cannot resolve, so the report can say so and
// print a manual hint instead of promising a fix (FR-039).
func (c CheckResult) NotFixable(hint string) CheckResult {
	c.Fixable = false
	c.Reason = hint

	return c
}

// Summary counts the outcomes of a run.
type Summary struct {
	Pass int
	Fail int
	Skip int
}

// Report is everything one run observed.
type Report struct {
	Command    string
	Repository string
	Instance   config.Instance
	Profiles   []string
	Checks     []CheckResult
	Summary    Summary
	Warnings   []string

	// Actions is populated by apply only, and left empty by check.
	Actions []ActionResult
}

// Summarise recomputes the summary from the checks.
func (r *Report) Summarise() {
	r.Summary = Summary{}

	for _, c := range r.Checks {
		switch c.Status {
		case StatusPass:
			r.Summary.Pass++
		case StatusFail:
			r.Summary.Fail++
		case StatusSkip:
			r.Summary.Skip++
		}
	}
}

// Warnf records a non-fatal condition. The same text goes to stderr, and is
// repeated in the machine-readable document so a JSON consumer sees it without
// reading two streams.
func (r *Report) Warnf(format string, args ...any) {
	r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
}

// Drifted reports whether any check failed. A run whose checks are all pass or
// skip is compliant: a skip is a check that could not be evaluated, never drift
// (FR-030, and the "check outcome with skips only" assumption).
func (r *Report) Drifted() bool { return r.Summary.Fail > 0 }

// ExitCode derives the process outcome from the report: 3 when drift remains,
// 0 when the repository is compliant. A runtime failure is not a report at all
// and is classified from the error instead (CLI-002).
func (r *Report) ExitCode() int {
	if r.Drifted() {
		return ExitDrift
	}

	return ExitOK
}

// The two exit codes a completed run can produce. The other two — 1 for a
// runtime failure and 2 for a usage error — come from an error, never from a
// report (CLI-002, R11).
const (
	ExitOK    = 0
	ExitDrift = 3
)
