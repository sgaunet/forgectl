package gitlab_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sgaunet/forgectl/internal/config"
	"github.com/sgaunet/forgectl/internal/forge"
	"github.com/sgaunet/forgectl/internal/forge/gitlab"
)

// The project id GitLab takes is owner/repo, URL-encoded. Go's http server
// decodes it before routing, so the route keys below carry the decoded form.
const projectPath = "/api/v4/projects/acme/my-tool"

// routes maps a "METHOD /path" key onto the handler answering it. Any request
// the test did not plan for fails loudly.
type routes map[string]http.HandlerFunc

// newClient starts a server serving the given routes and returns a client
// pointed at it.
func newClient(t *testing.T, r routes) (*gitlab.Client, *[]string) {
	t.Helper()

	var seen []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		key := req.Method + " " + req.URL.Path
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

	client, err := gitlab.NewAt(srv.URL+"/api/v4", "acme/my-tool")
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}

	return client, &seen
}

// status answers with a status and a GitLab-shaped error body.
func status(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`{"message":"404 Not found"}`))
	}
}

func TestDefaultBranch(t *testing.T) {
	client, seen := newClient(t, routes{
		"GET " + projectPath: replies(`{"id":42,"default_branch":"master"}`),
	})

	got, err := client.DefaultBranch(context.Background())
	if err != nil {
		t.Fatalf("DefaultBranch: %v", err)
	}
	if got != "master" {
		t.Errorf("DefaultBranch = %q, want master", got)
	}

	// The project id is owner/repo, URL-encoded on the wire.
	if len(*seen) != 1 || (*seen)[0] != "GET "+projectPath {
		t.Errorf("calls = %v, want a single GET on the project", *seen)
	}
}

func TestBranchExists(t *testing.T) {
	client, _ := newClient(t, routes{
		"GET " + projectPath + "/repository/branches/main":   replies(`{"name":"main"}`),
		"GET " + projectPath + "/repository/branches/absent": status(http.StatusNotFound),
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

func TestProtectionMapsAccessLevels(t *testing.T) {
	tests := []struct {
		name  string
		level int
		want  config.AccessLevel
	}{
		{name: "none", level: 0, want: config.AccessNone},
		{name: "developer", level: 30, want: config.AccessDeveloper},
		{name: "maintainer", level: 40, want: config.AccessMaintainer},
		// An owner-level grant satisfies a maintainer requirement; forgectl's
		// vocabulary stops at maintainer.
		{name: "owner is reported as maintainer", level: 60, want: config.AccessMaintainer},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, _ := newClient(t, routes{
				"GET " + projectPath + "/protected_branches/main": func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"id":1,"name":"main","allow_force_push":false,
						"push_access_levels":[{"access_level":` + itoa(tt.level) + `}]}`))
				},
			})

			got, err := client.Protection(context.Background(), "main")
			if err != nil {
				t.Fatalf("Protection: %v", err)
			}
			if got.PushAccessLevel != tt.want {
				t.Errorf("push access level = %q, want %q", got.PushAccessLevel, tt.want)
			}
		})
	}
}

func TestDeletionIsAlwaysDeniedOnGitLab(t *testing.T) {
	// R9: GitLab always denies deleting a protected branch, and offers no
	// toggle, so the client reports AllowDelete false unconditionally.
	client, _ := newClient(t, routes{
		"GET " + projectPath + "/protected_branches/main": replies(
			`{"id":1,"name":"main","allow_force_push":true,
			  "push_access_levels":[{"access_level":40}]}`),
	})

	got, err := client.Protection(context.Background(), "main")
	if err != nil {
		t.Fatalf("Protection: %v", err)
	}
	if got.AllowDelete {
		t.Error("AllowDelete = true; deleting a protected branch is always denied on GitLab")
	}
	if !got.AllowForcePush {
		t.Error("AllowForcePush = false, want the platform's own value")
	}
}

func TestProtectionOnAnUnprotectedBranch(t *testing.T) {
	client, _ := newClient(t, routes{
		"GET " + projectPath + "/protected_branches/main": status(http.StatusNotFound),
	})

	got, err := client.Protection(context.Background(), "main")
	if err != nil {
		t.Fatalf("Protection returned an error for an unprotected branch: %v", err)
	}
	if got.Exists {
		t.Error("Exists = true for a branch with no protection entry")
	}
}

func TestPerUserGrantsAreNotTreatedAsALevel(t *testing.T) {
	// A per-user or per-group grant is an exception, not a level, and forgectl
	// does not model exceptions.
	client, _ := newClient(t, routes{
		"GET " + projectPath + "/protected_branches/main": replies(
			`{"id":1,"name":"main","allow_force_push":false,
			  "push_access_levels":[{"access_level":40,"user_id":7},{"access_level":30}]}`),
	})

	got, err := client.Protection(context.Background(), "main")
	if err != nil {
		t.Fatalf("Protection: %v", err)
	}
	if got.PushAccessLevel != config.AccessDeveloper {
		t.Errorf("push access level = %q, want developer: the user grant is not a level",
			got.PushAccessLevel)
	}
}

func TestTagProtectionFollowsPagination(t *testing.T) {
	var pages int

	client, _ := newClient(t, routes{
		"GET " + projectPath + "/protected_tags": func(w http.ResponseWriter, r *http.Request) {
			pages++
			w.Header().Set("Content-Type", "application/json")

			if r.URL.Query().Get("page") == "" {
				w.Header().Set("X-Next-Page", "2")
				_, _ = w.Write([]byte(`[{"name":"v*"}]`))

				return
			}
			_, _ = w.Write([]byte(`[{"name":"release-*"}]`))
		},
	})

	got, err := client.TagProtection(context.Background())
	if err != nil {
		t.Fatalf("TagProtection: %v", err)
	}

	if pages < 2 {
		t.Errorf("the listing stopped after %d page(s); pagination was not followed", pages)
	}
	if len(got) != 2 || got[0] != "v*" || got[1] != "release-*" {
		t.Errorf("TagProtection = %v, want both pages", got)
	}
}

func TestUnauthorisedBecomesInsufficientRights(t *testing.T) {
	// FR-030: a credential without the right must let the check skip with a
	// reason rather than fail the run, which needs a classifiable error.
	client, _ := newClient(t, routes{
		"GET " + projectPath: status(http.StatusForbidden),
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
		"GET " + projectPath: status(http.StatusForbidden),
	})

	_, err := client.DefaultBranch(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	// FR-054: no credential in any error string.
	if got := err.Error(); strings.Contains(got, "test-token") || strings.Contains(got, "PRIVATE-TOKEN") {
		t.Errorf("the error message carries credential material: %q", got)
	}
}

// itoa keeps the JSON fixtures above readable without importing strconv into
// every literal.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}

	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}

	return string(digits)
}
