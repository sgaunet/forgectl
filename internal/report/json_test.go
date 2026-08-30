package report_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/sgaunet/forgectl/internal/compliance"
	"github.com/sgaunet/forgectl/internal/config"
	"github.com/sgaunet/forgectl/internal/report"
)

// schemaPath is the contract this package's output must satisfy. Reading the
// contract itself, rather than a copy of it, is what keeps the two from
// drifting apart.
const schemaPath = "../../specs/001-forge-conventions/contracts/output.schema.json"

// schema is the subset of JSON Schema the contract uses: required, enum,
// pattern, type, and additionalProperties. Validating against it by hand keeps
// the approved dependency list at six (R12).
type schema struct {
	Type                 string            `json:"type"`
	Required             []string          `json:"required"`
	Properties           map[string]schema `json:"properties"`
	AdditionalProperties *bool             `json:"additionalProperties"` //nolint:tagliatelle // JSON Schema's own spelling
	Enum                 []any             `json:"enum"`
	Pattern              string            `json:"pattern"`
	Items                *schema           `json:"items"`
	Ref                  string            `json:"$ref"`
	Defs                 map[string]schema `json:"$defs"`
}

// loadSchema reads the contract.
func loadSchema(t *testing.T) schema {
	t.Helper()

	data, err := os.ReadFile(filepath.Clean(schemaPath))
	if err != nil {
		t.Fatalf("reading the output schema: %v", err)
	}

	var s schema
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("parsing the output schema: %v", err)
	}

	return s
}

// validate checks one value against a schema node, reporting every violation it
// finds rather than the first.
func validate(t *testing.T, defs map[string]schema, s schema, value any, path string) {
	t.Helper()

	if s.Ref != "" {
		name := filepath.Base(s.Ref)
		resolved, ok := defs[name]
		if !ok {
			t.Fatalf("%s: the schema references $defs/%s, which does not exist", path, name)
		}
		validate(t, defs, resolved, value, path)

		return
	}

	if len(s.Enum) > 0 {
		validateEnum(t, s, value, path)
	}
	if s.Pattern != "" {
		validatePattern(t, s, value, path)
	}

	switch typed := value.(type) {
	case map[string]any:
		validateObject(t, defs, s, typed, path)
	case []any:
		if s.Items != nil {
			for i, item := range typed {
				validate(t, defs, *s.Items, item, fmt.Sprintf("%s[%d]", path, i))
			}
		}
	}
}

// validateEnum checks a value against the permitted set.
func validateEnum(t *testing.T, s schema, value any, path string) {
	t.Helper()

	for _, allowed := range s.Enum {
		if allowed == value {
			return
		}
	}

	t.Errorf("%s = %v, which the schema's enum %v does not permit", path, value, s.Enum)
}

// validatePattern checks a string against the declared pattern.
func validatePattern(t *testing.T, s schema, value any, path string) {
	t.Helper()

	str, ok := value.(string)
	if !ok {
		return
	}

	re, err := regexp.Compile(s.Pattern)
	if err != nil {
		t.Fatalf("%s: the schema's pattern %q does not compile: %v", path, s.Pattern, err)
	}
	if !re.MatchString(str) {
		t.Errorf("%s = %q, which does not match the schema's pattern %q", path, str, s.Pattern)
	}
}

// validateObject checks required properties, forbidden extras, and each
// property in turn.
func validateObject(t *testing.T, defs map[string]schema, s schema, obj map[string]any, path string) {
	t.Helper()

	for _, required := range s.Required {
		if _, ok := obj[required]; !ok {
			t.Errorf("%s is missing the required property %q", path, required)
		}
	}

	for key, val := range obj {
		prop, declared := s.Properties[key]
		if !declared {
			if s.AdditionalProperties != nil && !*s.AdditionalProperties {
				t.Errorf("%s declares undeclared property %q; the schema forbids extras", path, key)
			}

			continue
		}
		validate(t, defs, prop, val, path+"."+key)
	}
}

// fullReport is a report exercising every branch of the projection: a pass, a
// fail, a not-fixable fail, a skip, a tags check, a generated variable, and an
// executed action.
func fullReport() *compliance.Report {
	fixable := compliance.Fail(compliance.CheckBranch, compliance.DomainBranch, "main", "master")
	notFixable := compliance.Fail(compliance.CheckBranch, compliance.DomainBranch, "main", "trunk").
		NotFixable("rename it by hand, then rerun")

	tags := compliance.Pass(compliance.CheckTags, compliance.DomainProtection)
	tags.Pattern = "v*"

	generated := compliance.Pass(compliance.VarCheckID("GITLAB_TOKEN"), compliance.DomainVars)
	generated.Generator = &compliance.GeneratorStatus{
		Kind:         config.GeneratorKindGitLabPAT,
		ExpiresAt:    time.Date(2026, 12, 25, 0, 0, 0, 0, time.UTC),
		RotateInDays: 117,
	}

	r := &compliance.Report{
		Command:    "apply",
		Repository: "acme/my-tool",
		Instance: config.Instance{
			Name: "gitlab.com", Platform: config.PlatformGitLab,
			Host: "gitlab.com", APIURL: "https://gitlab.com/api/v4",
			TokenEnv: "GITLAB_TOKEN",
		},
		Profiles: []string{"go-release"},
		Checks: []compliance.CheckResult{
			fixable,
			notFixable,
			compliance.Skip(compliance.CheckProtection, compliance.DomainProtection,
				"branch main does not exist on the platform"),
			tags,
			generated,
			compliance.Fail(compliance.VarCheckID("GALAXY_API_TOKEN"), compliance.DomainVars,
				"present", "missing"),
		},
		Actions: []compliance.ActionResult{
			{
				Action: compliance.Action{
					Kind: compliance.ActionSetDefaultBranch, Domain: compliance.DomainBranch,
					Description: "set the platform default branch to main", Destructive: true,
				},
				Status: compliance.ActionDone,
			},
			{
				Action: compliance.Action{
					Kind: compliance.ActionSetVariable, Domain: compliance.DomainVars,
					Description: "write CI variable GALAXY_API_TOKEN",
				},
				Status: compliance.ActionFailed,
				Error:  "the platform rejected the write",
			},
		},
	}
	r.Warnf("no profile was selected, so CI variables were not checked")
	r.Summarise()

	return r
}

func TestJSONConformsToTheContract(t *testing.T) {
	s := loadSchema(t)

	var buf bytes.Buffer
	if err := report.WriteJSON(&buf, report.FromReport(fullReport())); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	var doc any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("the JSON report does not parse: %v\n%s", err, buf.String())
	}

	validate(t, s.Defs, s, doc, "$")
}

func TestJSONIsOneDocumentPerRun(t *testing.T) {
	var buf bytes.Buffer
	if err := report.WriteJSON(&buf, report.FromReport(fullReport())); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	// A caller pipes this into jq, so the stream must hold exactly one value.
	dec := json.NewDecoder(&buf)

	var first any
	if err := dec.Decode(&first); err != nil {
		t.Fatalf("decoding the first value: %v", err)
	}

	var second any
	if err := dec.Decode(&second); err == nil {
		t.Error("the output carries more than one JSON value")
	}
}

func TestCheckAndApplyShareOneShape(t *testing.T) {
	// FR-055: one schema serves both commands. The only difference is that
	// apply populates actions.
	r := fullReport()
	r.Command = "check"
	r.Actions = nil

	doc := report.FromReport(r)
	if len(doc.Actions) != 0 {
		t.Errorf("check produced %d actions, want none", len(doc.Actions))
	}

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if bytes.Contains(data, []byte(`"actions"`)) {
		t.Error("check emitted an actions key; it must be omitted rather than null")
	}
}

func TestFixableIsReportedOnlyForFailures(t *testing.T) {
	doc := report.FromReport(fullReport())

	for _, c := range doc.Checks {
		switch c.Status {
		case "fail":
			if c.Fixable == nil {
				t.Errorf("check %q failed but reports no fixable flag", c.ID)
			}
		default:
			if c.Fixable != nil {
				t.Errorf("check %q is %s yet reports a fixable flag", c.ID, c.Status)
			}
		}
	}
}

func TestGeneratorFieldsAreCarried(t *testing.T) {
	// FR-055: a generated variable's entry carries its generator name, expiry
	// date, and remaining days.
	doc := report.FromReport(fullReport())

	var found bool
	for _, c := range doc.Checks {
		if c.Generator == "" {
			continue
		}
		found = true

		if c.Generator != config.GeneratorKindGitLabPAT {
			t.Errorf("generator = %q, want %q", c.Generator, config.GeneratorKindGitLabPAT)
		}
		if c.ExpiresAt != "2026-12-25" {
			t.Errorf("expires_at = %q, want 2026-12-25", c.ExpiresAt)
		}
		if c.RotateInDays == nil || *c.RotateInDays != 117 {
			t.Errorf("rotate_in_days = %v, want 117", c.RotateInDays)
		}
	}

	if !found {
		t.Error("no generated variable reached the document")
	}
}
