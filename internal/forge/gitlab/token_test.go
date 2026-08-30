package gitlab_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sgaunet/forgectl/internal/config"
	"github.com/sgaunet/forgectl/internal/forge"
)

const tokensPath = projectPath + "/access_tokens"

func TestProjectTokensListsOnlyActiveOnesOfThatName(t *testing.T) {
	// FR-028: a revoked token of the same name is history, not a duplicate.
	// Counting it would make every project that has ever rotated look ambiguous.
	client, _ := newClient(t, routes{
		"GET " + tokensPath: replies(`[
			{"id":1,"name":"forgectl","active":true,"revoked":false,"expires_at":"2027-01-01"},
			{"id":2,"name":"forgectl","active":false,"revoked":true,"expires_at":"2025-01-01"},
			{"id":3,"name":"someone-elses","active":true,"revoked":false,"expires_at":"2027-01-01"}
		]`),
	})

	got, err := client.ProjectTokens(context.Background(), "forgectl")
	if err != nil {
		t.Fatalf("ProjectTokens: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("tokens = %d, want 1 active token named forgectl", len(got))
	}
	if got[0].ID != 1 {
		t.Errorf("token id = %d, want 1", got[0].ID)
	}
	if got[0].ExpiresAt.Format(time.DateOnly) != "2027-01-01" {
		t.Errorf("expires_at = %v, want 2027-01-01", got[0].ExpiresAt)
	}
}

func TestProjectTokenListingFollowsPagination(t *testing.T) {
	var pages int

	client, _ := newClient(t, routes{
		"GET " + tokensPath: func(w http.ResponseWriter, r *http.Request) {
			pages++
			w.Header().Set("Content-Type", "application/json")

			if r.URL.Query().Get("page") == "" {
				w.Header().Set("X-Next-Page", "2")
				fmt.Fprint(w, `[{"id":1,"name":"forgectl","active":true,"expires_at":"2027-01-01"}]`)

				return
			}
			fmt.Fprint(w, `[{"id":2,"name":"forgectl","active":true,"expires_at":"2027-01-01"}]`)
		},
	})

	got, err := client.ProjectTokens(context.Background(), "forgectl")
	if err != nil {
		t.Fatalf("ProjectTokens: %v", err)
	}

	if pages < 2 {
		t.Errorf("the listing stopped after %d page(s)", pages)
	}
	if len(got) != 2 {
		t.Errorf("tokens = %d, want both pages", len(got))
	}
}

func TestCreateProjectTokenSendsTheConfiguredLifecycle(t *testing.T) {
	// R8: name, scopes, access_level, and an explicit expires_at as YYYY-MM-DD.
	var sent []captured

	client, _ := newClient(t, routes{
		"POST " + tokensPath: capture(t, &sent,
			`{"id":7,"name":"forgectl","active":true,"expires_at":"2027-02-26","token":"glpat-brand-new"}`),
	})

	expires := time.Date(2027, 2, 26, 0, 0, 0, 0, time.UTC)

	token, value, err := client.CreateProjectToken(context.Background(), forge.ProjectTokenRequest{
		Name:      "forgectl",
		Scopes:    []string{"api"},
		Role:      config.AccessMaintainer,
		ExpiresAt: expires,
	})
	if err != nil {
		t.Fatalf("CreateProjectToken: %v", err)
	}

	body := sent[0].Body
	if body["name"] != "forgectl" {
		t.Errorf("name = %v, want forgectl", body["name"])
	}
	if body["access_level"] != float64(40) {
		t.Errorf("access_level = %v, want 40 (maintainer)", body["access_level"])
	}
	if body["expires_at"] != "2027-02-26" {
		t.Errorf("expires_at = %v, want the calendar date 2027-02-26", body["expires_at"])
	}

	scopes, ok := body["scopes"].([]any)
	if !ok || len(scopes) != 1 || scopes[0] != "api" {
		t.Errorf("scopes = %v, want [api]", body["scopes"])
	}

	// The value is disclosed exactly once, here, and returned to the caller
	// that writes it straight into the CI variable (R8, FR-047).
	if value != "glpat-brand-new" {
		t.Errorf("the creation response's token was not returned: %q", value)
	}
	if token.ID != 7 {
		t.Errorf("token id = %d, want 7", token.ID)
	}
}

func TestCreateAlwaysSendsAnExplicitExpiry(t *testing.T) {
	// forgectl never relies on the instance default: the lifetime a maintainer
	// configured is the one they get (R8).
	var sent []captured

	client, _ := newClient(t, routes{
		"POST " + tokensPath: capture(t, &sent, `{"id":1,"name":"forgectl","active":true}`),
	})

	_, _, err := client.CreateProjectToken(context.Background(), forge.ProjectTokenRequest{
		Name: "forgectl", Scopes: []string{"api"}, Role: config.AccessDeveloper,
		ExpiresAt: time.Now().AddDate(0, 0, 180),
	})
	if err != nil {
		t.Fatalf("CreateProjectToken: %v", err)
	}

	if _, present := sent[0].Body["expires_at"]; !present {
		t.Error("expires_at was omitted; the instance default must never be relied on")
	}
	if sent[0].Body["access_level"] != float64(30) {
		t.Errorf("access_level = %v, want 30 (developer)", sent[0].Body["access_level"])
	}
}

func TestLifetimeAboveTheInstanceMaximumIsAnExplicitError(t *testing.T) {
	// FR-052: the message states the maximum permitted, which is the platform's
	// own wording, surfaced verbatim.
	const message = "The expiration date must be within the allowed lifetime of 365 days."

	client, _ := newClient(t, routes{
		"POST " + tokensPath: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"message":%q}`, message)
		},
	})

	_, _, err := client.CreateProjectToken(context.Background(), forge.ProjectTokenRequest{
		Name: "forgectl", Scopes: []string{"api"}, Role: config.AccessMaintainer,
		ExpiresAt: time.Now().AddDate(2, 0, 0),
	})
	if err == nil {
		t.Fatal("a lifetime above the maximum was accepted")
	}
	if !errors.Is(err, forge.ErrTokenLifetime) {
		t.Fatalf("error %v does not wrap ErrTokenLifetime", err)
	}
	if !strings.Contains(err.Error(), "365") {
		t.Errorf("message %q does not state the permitted maximum", err.Error())
	}
}

func TestRevokeProjectToken(t *testing.T) {
	var seen []string

	client, _ := newClient(t, routes{
		"DELETE " + tokensPath + "/7": func(w http.ResponseWriter, r *http.Request) {
			seen = append(seen, r.Method+" "+r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		},
	})

	if err := client.RevokeProjectToken(context.Background(), 7); err != nil {
		t.Fatalf("RevokeProjectToken: %v", err)
	}

	if len(seen) != 1 {
		t.Errorf("requests = %v, want a single DELETE on the token", seen)
	}
}

func TestInsufficientRightsIsDistinguishable(t *testing.T) {
	// FR-030: a credential that may not list tokens must let the check SKIP
	// with that reason rather than fail the run, which needs a classifiable
	// error.
	client, _ := newClient(t, routes{
		"GET " + tokensPath: status(http.StatusForbidden),
	})

	_, err := client.ProjectTokens(context.Background(), "forgectl")
	if err == nil {
		t.Fatal("a 403 was not reported as an error")
	}
	if !errors.Is(err, forge.ErrInsufficientRights) {
		t.Errorf("error %v does not wrap ErrInsufficientRights", err)
	}
}

func TestTokenErrorsNeverCarryTheValue(t *testing.T) {
	client, _ := newClient(t, routes{
		"POST " + tokensPath: status(http.StatusInternalServerError),
	})

	_, _, err := client.CreateProjectToken(context.Background(), forge.ProjectTokenRequest{
		Name: "forgectl", Scopes: []string{"api"}, Role: config.AccessMaintainer,
		ExpiresAt: time.Now().AddDate(0, 0, 180),
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "glpat-") {
		t.Errorf("the error carries token material: %q", err.Error())
	}
}
