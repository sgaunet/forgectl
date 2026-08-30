package github_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/crypto/nacl/box"

	"github.com/sgaunet/forgectl/internal/forge"
)

// publicKeyReply builds the public-key response GitHub returns, and hands back
// the private half so a test can open what was sealed.
func publicKeyReply(t *testing.T) (http.HandlerFunc, *[32]byte, *[32]byte) {
	t.Helper()

	public, private, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	body := `{"key_id":"key-1","key":"` + base64.StdEncoding.EncodeToString(public[:]) + `"}`

	return replies(body), public, private
}

func TestSecretVariableRoutesToTheCredentialStore(t *testing.T) {
	// FR-026: secret: true is an Actions credential, secret: false an Actions
	// variable. The route table proves which endpoint was used.
	keyHandler, public, private := publicKeyReply(t)

	var sent []captured

	client, _ := newClient(t, routes{
		"GET /repos/acme/my-tool/actions/secrets/public-key": keyHandler,
		"PUT /repos/acme/my-tool/actions/secrets/TOKEN":      capture(t, &sent, `{}`),
	})

	err := client.SetVariable(context.Background(), forge.VariableWrite{
		Name: "TOKEN", Value: "the-value", Secret: true,
	})
	if err != nil {
		t.Fatalf("SetVariable: %v", err)
	}

	if len(sent) != 1 {
		t.Fatalf("requests = %d, want 1", len(sent))
	}

	body := sent[0].Body
	if body["key_id"] != "key-1" {
		t.Errorf("key_id = %v, want key-1", body["key_id"])
	}

	encrypted, ok := body["encrypted_value"].(string)
	if !ok {
		t.Fatalf("no encrypted_value in %v", body)
	}

	// The value went over the wire sealed, and only sealed.
	if strings.Contains(encrypted, "the-value") {
		t.Error("the plaintext appears in the request body")
	}

	ciphertext, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		t.Fatalf("encrypted_value is not base64: %v", err)
	}

	opened, ok := box.OpenAnonymous(nil, ciphertext, public, private)
	if !ok {
		t.Fatal("the sealed value could not be opened with the advertised key")
	}
	if string(opened) != "the-value" {
		t.Errorf("opened %q, want the-value", opened)
	}
}

func TestNonSecretVariableRoutesToTheVariableStore(t *testing.T) {
	var sent []captured

	client, _ := newClient(t, routes{
		"GET /repos/acme/my-tool/actions/variables/MODE": status(http.StatusNotFound),
		"POST /repos/acme/my-tool/actions/variables":     capture(t, &sent, `{}`),
	})

	err := client.SetVariable(context.Background(), forge.VariableWrite{
		Name: "MODE", Value: "release", Secret: false,
	})
	if err != nil {
		t.Fatalf("SetVariable: %v", err)
	}

	if len(sent) != 1 {
		t.Fatalf("requests = %d, want a single POST", len(sent))
	}
	if sent[0].Body["name"] != "MODE" || sent[0].Body["value"] != "release" {
		t.Errorf("body = %v, want the name and value", sent[0].Body)
	}
}

func TestAnExistingVariableIsUpdatedRatherThanRecreated(t *testing.T) {
	var sent []captured

	client, _ := newClient(t, routes{
		"GET /repos/acme/my-tool/actions/variables/MODE":   replies(`{"name":"MODE","value":"old"}`),
		"PATCH /repos/acme/my-tool/actions/variables/MODE": capture(t, &sent, `{}`),
	})

	err := client.SetVariable(context.Background(), forge.VariableWrite{
		Name: "MODE", Value: "release", Secret: false,
	})
	if err != nil {
		t.Fatalf("SetVariable: %v", err)
	}

	if len(sent) != 1 {
		t.Fatalf("requests = %+v, want a single update", sent)
	}
	if sent[0].Body["value"] != "release" {
		t.Errorf("value = %v, want release", sent[0].Body["value"])
	}
}

func TestReadingASecretDisclosesNoValue(t *testing.T) {
	// FR-027: GitHub's credentials are write-only, so ValueReadable is false
	// and the comparison in the compliance layer never runs.
	client, _ := newClient(t, routes{
		"GET /repos/acme/my-tool/actions/secrets/TOKEN": replies(
			`{"name":"TOKEN","created_at":"2026-01-01T00:00:00Z"}`),
	})

	got, err := client.Variable(context.Background(), "TOKEN", true)
	if err != nil {
		t.Fatalf("Variable: %v", err)
	}

	if !got.Exists {
		t.Error("Exists = false for a credential the platform reports")
	}
	if got.ValueReadable {
		t.Error("ValueReadable = true; a GitHub Actions credential cannot be read back")
	}
	if got.Value != "" {
		t.Errorf("Value = %q, want empty", got.Value)
	}
}

func TestReadingAVariableDisclosesItsValue(t *testing.T) {
	client, _ := newClient(t, routes{
		"GET /repos/acme/my-tool/actions/variables/MODE": replies(
			`{"name":"MODE","value":"release"}`),
	})

	got, err := client.Variable(context.Background(), "MODE", false)
	if err != nil {
		t.Fatalf("Variable: %v", err)
	}

	if !got.ValueReadable || got.Value != "release" {
		t.Errorf("got %+v, want the value readable and equal to release", got)
	}
}

func TestAnAbsentVariableIsReportedAsMissing(t *testing.T) {
	// A 404 means absent, not an error (contracts/forge-endpoints.md).
	client, _ := newClient(t, routes{
		"GET /repos/acme/my-tool/actions/secrets/TOKEN":  status(http.StatusNotFound),
		"GET /repos/acme/my-tool/actions/variables/MODE": status(http.StatusNotFound),
	})

	for _, tt := range []struct {
		name   string
		secret bool
	}{
		{name: "TOKEN", secret: true},
		{name: "MODE", secret: false},
	} {
		got, err := client.Variable(context.Background(), tt.name, tt.secret)
		if err != nil {
			t.Errorf("Variable(%s) returned an error for a 404: %v", tt.name, err)
		}
		if got.Exists {
			t.Errorf("Variable(%s).Exists = true for a 404", tt.name)
		}
	}
}

func TestMaskedAndProtectedAreNeverReportedOnGitHub(t *testing.T) {
	// FR-026: GitHub models neither, so both stay at their zero value and the
	// compliance layer never compares them.
	client, _ := newClient(t, routes{
		"GET /repos/acme/my-tool/actions/variables/MODE": replies(
			`{"name":"MODE","value":"release"}`),
	})

	got, err := client.Variable(context.Background(), "MODE", false)
	if err != nil {
		t.Fatalf("Variable: %v", err)
	}

	if got.Masked || got.Protected {
		t.Errorf("got masked=%v protected=%v, want both false on GitHub", got.Masked, got.Protected)
	}
}
