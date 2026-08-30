package gitlab_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/sgaunet/forgectl/internal/config"
	"github.com/sgaunet/forgectl/internal/forge"
)

// captured is one request the test server recorded.
type captured struct {
	Method string
	Path   string
	Body   map[string]any
}

// readBody parses a request body as JSON, failing the test if it is not.
func readBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()

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

	return body
}

// capture records the request and answers with the given body.
func capture(t *testing.T, into *[]captured, reply string) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		*into = append(*into, captured{Method: r.Method, Path: r.URL.Path, Body: readBody(t, r)})

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reply))
	}
}

func TestSetDefaultBranch(t *testing.T) {
	var sent []captured

	client, _ := newClient(t, routes{
		"PUT " + projectPath: capture(t, &sent, `{"id":1,"default_branch":"main"}`),
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

func TestSetProtectionCreatesTheEntryWhenThereIsNone(t *testing.T) {
	var sent []captured

	client, _ := newClient(t, routes{
		"GET " + projectPath + "/protected_branches/main": status(http.StatusNotFound),
		"POST " + projectPath + "/protected_branches":     capture(t, &sent, `{"id":1,"name":"main"}`),
	})

	err := client.SetProtection(context.Background(), "main", forge.Protection{
		Exists: true, AllowForcePush: false, PushAccessLevel: config.AccessMaintainer,
	})
	if err != nil {
		t.Fatalf("SetProtection: %v", err)
	}

	if len(sent) != 1 || sent[0].Method != http.MethodPost {
		t.Fatalf("requests = %+v, want a POST", sent)
	}

	body := sent[0].Body
	if body["name"] != "main" {
		t.Errorf("name = %v, want main", body["name"])
	}
	if body["allow_force_push"] != false {
		t.Errorf("allow_force_push = %v, want false", body["allow_force_push"])
	}
	// R9: allow_delete is never sent — GitLab has no such toggle.
	if _, present := body["allow_delete"]; present {
		t.Error("allow_delete was sent; GitLab always denies deleting a protected branch")
	}
	// 40 is maintainer.
	if got := body["push_access_level"]; got != float64(40) {
		t.Errorf("push_access_level = %v, want 40 (maintainer)", got)
	}
}

func TestSetProtectionAmendsAnExistingEntry(t *testing.T) {
	// Unprotecting and reprotecting would leave the branch briefly unprotected,
	// which is precisely the state this tool exists to prevent.
	var sent []captured

	client, _ := newClient(t, routes{
		"GET " + projectPath + "/protected_branches/main": replies(
			`{"id":1,"name":"main","allow_force_push":true,
			  "push_access_levels":[{"access_level":30}]}`),
		"PATCH " + projectPath + "/protected_branches/main": capture(t, &sent, `{"id":1,"name":"main"}`),
	})

	err := client.SetProtection(context.Background(), "main", forge.Protection{
		Exists: true, AllowForcePush: false, PushAccessLevel: config.AccessMaintainer,
	})
	if err != nil {
		t.Fatalf("SetProtection: %v", err)
	}

	if len(sent) != 1 || sent[0].Method != http.MethodPatch {
		t.Fatalf("requests = %+v, want a PATCH on the existing entry", sent)
	}
	if sent[0].Body["allow_force_push"] != false {
		t.Errorf("allow_force_push = %v, want false", sent[0].Body["allow_force_push"])
	}
}

func TestPushAccessLevelIsSentAsTheNumericLevel(t *testing.T) {
	tests := []struct {
		name  string
		level config.AccessLevel
		want  float64
	}{
		{name: "none", level: config.AccessNone, want: 0},
		{name: "developer", level: config.AccessDeveloper, want: 30},
		{name: "maintainer", level: config.AccessMaintainer, want: 40},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sent []captured

			client, _ := newClient(t, routes{
				"GET " + projectPath + "/protected_branches/main": status(http.StatusNotFound),
				"POST " + projectPath + "/protected_branches":     capture(t, &sent, `{"id":1}`),
			})

			err := client.SetProtection(context.Background(), "main", forge.Protection{
				Exists: true, PushAccessLevel: tt.level,
			})
			if err != nil {
				t.Fatalf("SetProtection: %v", err)
			}

			if got := sent[0].Body["push_access_level"]; got != tt.want {
				t.Errorf("push_access_level = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProtectTag(t *testing.T) {
	var sent []captured

	client, _ := newClient(t, routes{
		"POST " + projectPath + "/protected_tags": capture(t, &sent, `{"name":"v*"}`),
	})

	if err := client.ProtectTag(context.Background(), "v*"); err != nil {
		t.Fatalf("ProtectTag: %v", err)
	}

	if got := sent[0].Body["name"]; got != "v*" {
		t.Errorf("name = %v, want v*", got)
	}
}

func TestProtectingAnAlreadyProtectedTagIsNotAFailure(t *testing.T) {
	// FR-035: a second apply on a converged repository must not fail. GitLab
	// rejects a duplicate protected tag with a conflict, which is the state
	// apply wanted.
	client, _ := newClient(t, routes{
		"POST " + projectPath + "/protected_tags": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"Protected tag 'v*' already exists"}`))
		},
	})

	if err := client.ProtectTag(context.Background(), "v*"); err != nil {
		t.Errorf("protecting an already-protected tag failed: %v", err)
	}
}

// replies answers with a fixed body.
func replies(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}
