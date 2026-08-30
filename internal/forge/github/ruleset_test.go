package github_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/sgaunet/forgectl/internal/forge"
)

// captured is one request body the test server recorded.
type captured struct {
	Method string
	Path   string
	Body   map[string]any
}

// capture records the request and answers with the given body.
func capture(t *testing.T, into *[]captured, reply string) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading the request body: %v", err)
		}

		var body map[string]any
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Errorf("the request body is not JSON: %v\n%s", err, raw)
			}
		}

		*into = append(*into, captured{Method: r.Method, Path: r.URL.Path, Body: body})

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reply))
	}
}

func TestSetDefaultBranch(t *testing.T) {
	var sent []captured

	client, _ := newClient(t, routes{
		"PATCH /repos/acme/my-tool": capture(t, &sent, `{"default_branch":"main"}`),
	})

	if err := client.SetDefaultBranch(context.Background(), "main"); err != nil {
		t.Fatalf("SetDefaultBranch: %v", err)
	}

	if len(sent) != 1 {
		t.Fatalf("requests = %d, want 1", len(sent))
	}
	if got := sent[0].Body["default_branch"]; got != "main" {
		t.Errorf("default_branch = %v, want main", got)
	}
}

func TestSetProtectionCreatesTheForgectlRuleset(t *testing.T) {
	var sent []captured

	client, _ := newClient(t, routes{
		// No ruleset exists yet.
		"GET /repos/acme/my-tool/rulesets":  replies(`[]`),
		"POST /repos/acme/my-tool/rulesets": capture(t, &sent, `{"id":1,"name":"forgectl"}`),
	})

	err := client.SetProtection(context.Background(), "main", forge.Protection{
		Exists: true, AllowForcePush: false, AllowDelete: false,
	})
	if err != nil {
		t.Fatalf("SetProtection: %v", err)
	}

	if len(sent) != 1 {
		t.Fatalf("requests = %d, want a single POST", len(sent))
	}

	body := sent[0].Body
	if body["name"] != "forgectl" {
		t.Errorf("name = %v, want forgectl", body["name"])
	}
	if body["target"] != "branch" {
		t.Errorf("target = %v, want branch", body["target"])
	}
	if body["enforcement"] != "active" {
		t.Errorf("enforcement = %v, want active", body["enforcement"])
	}

	includes := refIncludesOf(t, body)
	if len(includes) != 1 || includes[0] != "refs/heads/main" {
		t.Errorf("includes = %v, want [refs/heads/main]", includes)
	}

	rules := ruleTypesOf(t, body)
	if !rules["deletion"] || !rules["non_fast_forward"] {
		t.Errorf("rules = %v, want both deletion and non_fast_forward", rules)
	}
}

func TestAllowForcePushOmitsTheRuleRatherThanSettingIt(t *testing.T) {
	// R2: omitting a rule PERMITS the action, which is the only way GitHub's
	// rulesets express allow_force_push.
	var sent []captured

	client, _ := newClient(t, routes{
		"GET /repos/acme/my-tool/rulesets":  replies(`[]`),
		"POST /repos/acme/my-tool/rulesets": capture(t, &sent, `{"id":1}`),
	})

	err := client.SetProtection(context.Background(), "main", forge.Protection{
		Exists: true, AllowForcePush: true, AllowDelete: false,
	})
	if err != nil {
		t.Fatalf("SetProtection: %v", err)
	}

	rules := ruleTypesOf(t, sent[0].Body)
	if rules["non_fast_forward"] {
		t.Error("non_fast_forward was sent despite force-push being allowed")
	}
	if !rules["deletion"] {
		t.Error("deletion was omitted despite deletion being denied")
	}
}

func TestSetProtectionUpdatesTheRulesetItAlreadyOwns(t *testing.T) {
	var sent []captured

	client, _ := newClient(t, routes{
		"GET /repos/acme/my-tool/rulesets": replies(`[
			{"id":9,"name":"forgectl","target":"branch","enforcement":"active"}
		]`),
		"GET /repos/acme/my-tool/rulesets/9": replies(ruleset(
			9, "forgectl", "branch", "active", []string{"refs/heads/old"})),
		"PUT /repos/acme/my-tool/rulesets/9": capture(t, &sent, `{"id":9}`),
	})

	err := client.SetProtection(context.Background(), "main", forge.Protection{Exists: true})
	if err != nil {
		t.Fatalf("SetProtection: %v", err)
	}

	if len(sent) != 1 || sent[0].Method != http.MethodPut {
		t.Fatalf("requests = %+v, want a single PUT on the existing ruleset", sent)
	}
}

func TestARulesetUnderAnotherNameIsNeverModified(t *testing.T) {
	// research.md open item 2: forgectl modifies only the rulesets it created.
	var sent []captured

	client, _ := newClient(t, routes{
		"GET /repos/acme/my-tool/rulesets": replies(`[
			{"id":9,"name":"house-rules","target":"branch","enforcement":"active"}
		]`),
		"POST /repos/acme/my-tool/rulesets": capture(t, &sent, `{"id":10}`),
	})

	err := client.SetProtection(context.Background(), "main", forge.Protection{Exists: true})
	if err != nil {
		t.Fatalf("SetProtection: %v", err)
	}

	// It created its own rather than overwriting the maintainer's. A PUT on
	// ruleset 9 is not in the route table, so it would have failed the test.
	if len(sent) != 1 || sent[0].Method != http.MethodPost {
		t.Errorf("requests = %+v, want a POST creating forgectl's own ruleset", sent)
	}
}

func TestProtectTagCreatesATagRuleset(t *testing.T) {
	var sent []captured

	client, _ := newClient(t, routes{
		"GET /repos/acme/my-tool/rulesets":  replies(`[]`),
		"POST /repos/acme/my-tool/rulesets": capture(t, &sent, `{"id":1}`),
	})

	if err := client.ProtectTag(context.Background(), "v*"); err != nil {
		t.Fatalf("ProtectTag: %v", err)
	}

	body := sent[0].Body
	if body["target"] != "tag" {
		t.Errorf("target = %v, want tag", body["target"])
	}

	// A bare pattern is qualified into the ref form a condition takes.
	includes := refIncludesOf(t, body)
	if len(includes) != 1 || includes[0] != "refs/tags/v*" {
		t.Errorf("includes = %v, want [refs/tags/v*]", includes)
	}
}

func TestProtectingASecondTagKeepsTheFirst(t *testing.T) {
	// Protecting release-* must not silently unprotect v*.
	var sent []captured

	client, _ := newClient(t, routes{
		"GET /repos/acme/my-tool/rulesets": replies(`[
			{"id":4,"name":"forgectl","target":"tag","enforcement":"active"}
		]`),
		"GET /repos/acme/my-tool/rulesets/4": replies(ruleset(
			4, "forgectl", "tag", "active", []string{"refs/tags/v*"}, "deletion")),
		"PUT /repos/acme/my-tool/rulesets/4": capture(t, &sent, `{"id":4}`),
	})

	if err := client.ProtectTag(context.Background(), "release-*"); err != nil {
		t.Fatalf("ProtectTag: %v", err)
	}

	includes := refIncludesOf(t, sent[0].Body)
	if len(includes) != 2 {
		t.Fatalf("includes = %v, want both patterns", includes)
	}

	seen := map[string]bool{}
	for _, inc := range includes {
		seen[inc] = true
	}
	if !seen["refs/tags/v*"] || !seen["refs/tags/release-*"] {
		t.Errorf("includes = %v, want both refs/tags/v* and refs/tags/release-*", includes)
	}
}

// refIncludesOf digs the ref-name includes out of a captured request body.
func refIncludesOf(t *testing.T, body map[string]any) []string {
	t.Helper()

	conditions, ok := body["conditions"].(map[string]any)
	if !ok {
		t.Fatalf("the body carries no conditions: %v", body)
	}
	refName, ok := conditions["ref_name"].(map[string]any)
	if !ok {
		t.Fatalf("the conditions carry no ref_name: %v", conditions)
	}
	raw, ok := refName["include"].([]any)
	if !ok {
		t.Fatalf("ref_name carries no include: %v", refName)
	}

	out := make([]string, 0, len(raw))
	for _, item := range raw {
		out = append(out, item.(string)) //nolint:forcetypeassert // a JSON string array
	}

	return out
}

// ruleTypesOf collects the rule types a captured request body sends.
func ruleTypesOf(t *testing.T, body map[string]any) map[string]bool {
	t.Helper()

	raw, ok := body["rules"].([]any)
	if !ok {
		return map[string]bool{}
	}

	types := map[string]bool{}
	for _, item := range raw {
		rule, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := rule["type"].(string); ok {
			types[name] = true
		}
	}

	return types
}
