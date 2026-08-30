package github_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sgaunet/forgectl/internal/forge"
	"github.com/sgaunet/forgectl/internal/forge/github"
)

// routes maps a "METHOD /path" key onto the handler answering it. Any request
// the test did not plan for fails loudly, which is what catches a client that
// calls the wrong endpoint — the sunset tag-protection API above all (R2).
type routes map[string]http.HandlerFunc

// newClient starts a server serving the given routes and returns a client
// pointed at it.
func newClient(t *testing.T, r routes) (*github.Client, *[]string) {
	t.Helper()

	var seen []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// go-github's enterprise base URL carries an /api/v3 prefix. Stripping
		// it keeps the route keys below identical to the paths
		// contracts/forge-endpoints.md documents.
		key := req.Method + " " + strings.TrimPrefix(req.URL.Path, "/api/v3")
		seen = append(seen, key)

		handler, ok := r[key]
		if !ok {
			t.Errorf("unplanned request: %s", key)
			w.WriteHeader(http.StatusNotImplemented)

			return
		}
		handler(w, req)
	}))
	t.Cleanup(srv.Close)

	client, err := github.NewAt(srv.URL+"/", "acme", "my-tool")
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}

	return client, &seen
}

// replies answers with a fixed body.
func replies(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

// status answers with a status and an empty JSON body.
func status(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`{"message":"not found"}`))
	}
}

func TestDefaultBranch(t *testing.T) {
	client, _ := newClient(t, routes{
		"GET /repos/acme/my-tool": replies(`{"default_branch":"master"}`),
	})

	got, err := client.DefaultBranch(context.Background())
	if err != nil {
		t.Fatalf("DefaultBranch: %v", err)
	}
	if got != "master" {
		t.Errorf("DefaultBranch = %q, want master", got)
	}
}

func TestBranchExists(t *testing.T) {
	client, _ := newClient(t, routes{
		"GET /repos/acme/my-tool/branches/main":   replies(`{"name":"main"}`),
		"GET /repos/acme/my-tool/branches/absent": status(http.StatusNotFound),
	})

	exists, err := client.BranchExists(context.Background(), "main")
	if err != nil {
		t.Fatalf("BranchExists(main): %v", err)
	}
	if !exists {
		t.Error("BranchExists(main) = false, want true")
	}

	// A 404 is the answer "no", not a failure.
	exists, err = client.BranchExists(context.Background(), "absent")
	if err != nil {
		t.Fatalf("BranchExists(absent) returned an error for a 404: %v", err)
	}
	if exists {
		t.Error("BranchExists(absent) = true, want false")
	}
}

// ruleset renders a ruleset detail response.
func ruleset(id int, name, target, enforcement string, includes []string, rules ...string) string {
	quoted := make([]string, 0, len(includes))
	for _, inc := range includes {
		quoted = append(quoted, fmt.Sprintf("%q", inc))
	}

	ruleJSON := make([]string, 0, len(rules))
	for _, rule := range rules {
		ruleJSON = append(ruleJSON, fmt.Sprintf(`{"type":%q}`, rule))
	}

	return fmt.Sprintf(
		`{"id":%d,"name":%q,"target":%q,"source":"acme/my-tool","enforcement":%q,`+
			`"conditions":{"ref_name":{"include":[%s],"exclude":[]}},"rules":[%s]}`,
		id, name, target, enforcement, strings.Join(quoted, ","), strings.Join(ruleJSON, ","))
}

func TestProtectionReadsRulesetsNotTheSunsetTagAPI(t *testing.T) {
	// R2: the /tags/protection API returns NULL data since 2024-08-30, so
	// calling it appears to succeed while protecting nothing. The test server
	// fails any request to it.
	client, seen := newClient(t, routes{
		"GET /repos/acme/my-tool/rulesets": replies(`[
			{"id":1,"name":"forgectl","target":"branch","enforcement":"active"}
		]`),
		"GET /repos/acme/my-tool/rulesets/1": replies(ruleset(
			1, "forgectl", "branch", "active",
			[]string{"refs/heads/main"}, "deletion", "non_fast_forward")),
	})

	got, err := client.Protection(context.Background(), "main")
	if err != nil {
		t.Fatalf("Protection: %v", err)
	}

	if !got.Exists {
		t.Fatal("Protection reports the branch as unprotected")
	}
	if got.AllowForcePush {
		t.Error("non_fast_forward is present, so force-push must be denied")
	}
	if got.AllowDelete {
		t.Error("deletion is present, so deletion must be denied")
	}

	for _, call := range *seen {
		if call == "GET /repos/acme/my-tool/tags/protection" {
			t.Error("the client called the sunset tag protection API")
		}
	}
}

func TestAbsentRulePermitsTheAction(t *testing.T) {
	// R2: omitting a rule permits the action, which is how allow_force_push is
	// expressed.
	client, _ := newClient(t, routes{
		"GET /repos/acme/my-tool/rulesets": replies(`[
			{"id":1,"name":"forgectl","target":"branch","enforcement":"active"}
		]`),
		"GET /repos/acme/my-tool/rulesets/1": replies(ruleset(
			1, "forgectl", "branch", "active", []string{"refs/heads/main"}, "deletion")),
	})

	got, err := client.Protection(context.Background(), "main")
	if err != nil {
		t.Fatalf("Protection: %v", err)
	}
	if !got.AllowForcePush {
		t.Error("non_fast_forward is absent, so force-push must be reported as allowed")
	}
	if got.AllowDelete {
		t.Error("deletion is present, so deletion must be denied")
	}
}

func TestProtectionAcceptsARulesetForgectlDoesNotOwn(t *testing.T) {
	// forgectl verifies the effect, not its own authorship: a ruleset the
	// maintainer wrote that grants the required protection still passes
	// (research.md open item 2).
	client, _ := newClient(t, routes{
		"GET /repos/acme/my-tool/rulesets": replies(`[
			{"id":7,"name":"house-rules","target":"branch","enforcement":"active"}
		]`),
		"GET /repos/acme/my-tool/rulesets/7": replies(ruleset(
			7, "house-rules", "branch", "active",
			[]string{"refs/heads/main"}, "deletion", "non_fast_forward")),
	})

	got, err := client.Protection(context.Background(), "main")
	if err != nil {
		t.Fatalf("Protection: %v", err)
	}
	if !got.Exists || got.AllowForcePush || got.AllowDelete {
		t.Errorf("a ruleset under another name was ignored: %+v", got)
	}
}

func TestEvaluationIgnoresDisabledAndUnrelatedRulesets(t *testing.T) {
	client, _ := newClient(t, routes{
		"GET /repos/acme/my-tool/rulesets": replies(`[
			{"id":1,"name":"disabled","target":"branch","enforcement":"disabled"},
			{"id":2,"name":"other-branch","target":"branch","enforcement":"active"},
			{"id":3,"name":"tags","target":"tag","enforcement":"active"}
		]`),
		"GET /repos/acme/my-tool/rulesets/1": replies(ruleset(
			1, "disabled", "branch", "disabled",
			[]string{"refs/heads/main"}, "deletion", "non_fast_forward")),
		"GET /repos/acme/my-tool/rulesets/2": replies(ruleset(
			2, "other-branch", "branch", "active",
			[]string{"refs/heads/develop"}, "deletion", "non_fast_forward")),
		"GET /repos/acme/my-tool/rulesets/3": replies(ruleset(
			3, "tags", "tag", "active", []string{"refs/tags/v*"}, "deletion")),
	})

	got, err := client.Protection(context.Background(), "main")
	if err != nil {
		t.Fatalf("Protection: %v", err)
	}
	if got.Exists {
		t.Error("a disabled ruleset, or one covering another ref, was counted as protection")
	}
}

func TestProtectionHonoursTheRefNameAliases(t *testing.T) {
	for _, alias := range []string{"~ALL", "~DEFAULT_BRANCH"} {
		t.Run(alias, func(t *testing.T) {
			client, _ := newClient(t, routes{
				"GET /repos/acme/my-tool/rulesets": replies(`[
					{"id":1,"name":"forgectl","target":"branch","enforcement":"active"}
				]`),
				"GET /repos/acme/my-tool/rulesets/1": replies(ruleset(
					1, "forgectl", "branch", "active",
					[]string{alias}, "deletion", "non_fast_forward")),
			})

			got, err := client.Protection(context.Background(), "main")
			if err != nil {
				t.Fatalf("Protection: %v", err)
			}
			if !got.Exists {
				t.Errorf("the %s alias was not recognised as covering the branch", alias)
			}
		})
	}
}

func TestTagProtection(t *testing.T) {
	client, _ := newClient(t, routes{
		"GET /repos/acme/my-tool/rulesets": replies(`[
			{"id":1,"name":"forgectl","target":"tag","enforcement":"active"},
			{"id":2,"name":"names-only","target":"tag","enforcement":"active"}
		]`),
		"GET /repos/acme/my-tool/rulesets/1": replies(ruleset(
			1, "forgectl", "tag", "active", []string{"refs/tags/v*"}, "deletion")),
		// A ruleset that names a pattern while applying no protecting rule
		// protects nothing, and must not be counted.
		"GET /repos/acme/my-tool/rulesets/2": replies(ruleset(
			2, "names-only", "tag", "active", []string{"refs/tags/rc-*"})),
	})

	got, err := client.TagProtection(context.Background())
	if err != nil {
		t.Fatalf("TagProtection: %v", err)
	}

	if len(got) != 1 || got[0] != "refs/tags/v*" {
		t.Errorf("TagProtection = %v, want only the protecting ruleset's pattern", got)
	}
}

func TestRulesetListingFollowsPagination(t *testing.T) {
	var page int

	client, _ := newClient(t, routes{
		"GET /repos/acme/my-tool/rulesets": func(w http.ResponseWriter, r *http.Request) {
			page++
			if r.URL.Query().Get("page") == "" {
				next := "http://" + r.Host + r.URL.Path + "?page=2&per_page=100"
				w.Header().Set("Link", "<"+next+`>; rel="next"`)
				_, _ = w.Write([]byte(`[{"id":1,"name":"a","target":"tag","enforcement":"disabled"}]`))

				return
			}
			_, _ = w.Write([]byte(`[{"id":2,"name":"b","target":"tag","enforcement":"disabled"}]`))
		},
		"GET /repos/acme/my-tool/rulesets/1": replies(ruleset(1, "a", "tag", "disabled", nil)),
		"GET /repos/acme/my-tool/rulesets/2": replies(ruleset(2, "b", "tag", "disabled", nil)),
	})

	if _, err := client.TagProtection(context.Background()); err != nil {
		t.Fatalf("TagProtection: %v", err)
	}
	if page < 2 {
		t.Errorf("the listing stopped after %d page(s); pagination was not followed", page)
	}
}

func TestUnauthorisedBecomesInsufficientRights(t *testing.T) {
	// A 401 or 403 must be classifiable, so the token checks can skip with a
	// reason rather than fail the run (FR-030).
	client, _ := newClient(t, routes{
		"GET /repos/acme/my-tool": status(http.StatusForbidden),
	})

	_, err := client.DefaultBranch(context.Background())
	if err == nil {
		t.Fatal("a 403 was not reported as an error")
	}
	if !errors.Is(err, forge.ErrInsufficientRights) {
		t.Errorf("error %v does not wrap ErrInsufficientRights", err)
	}
}

func TestErrorsNeverCarryTheCredential(t *testing.T) {
	client, _ := newClient(t, routes{
		"GET /repos/acme/my-tool": status(http.StatusForbidden),
	})

	_, err := client.DefaultBranch(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	// FR-054: no value, and no credential, in any error string.
	if got := err.Error(); strings.Contains(got, "test-token") || strings.Contains(got, "Bearer") {
		t.Errorf("the error message carries credential material: %q", got)
	}
}
