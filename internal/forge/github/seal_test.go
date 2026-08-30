package github_test

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	"golang.org/x/crypto/nacl/box"

	"github.com/sgaunet/forgectl/internal/forge/github"
)

func TestSealProducesACiphertextTheRecipientCanOpen(t *testing.T) {
	public, private, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	const plaintext = "a-value-that-must-not-appear-anywhere"

	encoded, err := github.Seal(base64.StdEncoding.EncodeToString(public[:]), plaintext)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	// The ciphertext is what GitHub receives: base64 of a sealed box.
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("the sealed value is not base64: %v", err)
	}

	opened, ok := box.OpenAnonymous(nil, ciphertext, public, private)
	if !ok {
		t.Fatal("the recipient could not open the sealed box")
	}
	if string(opened) != plaintext {
		t.Errorf("opened %q, want %q", opened, plaintext)
	}
}

func TestSealNeverEmitsThePlaintext(t *testing.T) {
	public, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	const plaintext = "sentinel-value-0123456789"

	encoded, err := github.Seal(base64.StdEncoding.EncodeToString(public[:]), plaintext)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	if strings.Contains(encoded, plaintext) {
		t.Error("the sealed value contains the plaintext")
	}
}

func TestSealRejectsAMalformedKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{name: "not base64", key: "!!! not base64 !!!"},
		{name: "too short", key: base64.StdEncoding.EncodeToString([]byte("short"))},
		{name: "empty", key: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := github.Seal(tt.key, "a-value")
			if err == nil {
				t.Fatal("seal accepted a malformed public key")
			}
			// FR-054: not even a failure message may carry the value.
			if strings.Contains(err.Error(), "a-value") {
				t.Errorf("the error carries the plaintext: %q", err.Error())
			}
		})
	}
}

func TestSealIsNotDeterministic(t *testing.T) {
	// An anonymous sealed box carries an ephemeral public key, so the same
	// plaintext seals differently every time. A deterministic ciphertext would
	// let an observer tell that two repositories hold the same value.
	public, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	key := base64.StdEncoding.EncodeToString(public[:])

	first, err := github.Seal(key, "same-value")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	second, err := github.Seal(key, "same-value")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	if first == second {
		t.Error("sealing the same value twice produced the same ciphertext")
	}
}
