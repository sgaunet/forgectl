package github

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/nacl/box"
)

// keySize is the length of a NaCl public key.
const keySize = 32

// seal encrypts a value for a repository's Actions credential store.
//
// GitHub requires a libsodium sealed box: the repository's public key is
// fetched, the value is sealed to it anonymously, and the ciphertext is sent
// base64-encoded alongside the key's id. nacl/box.SealAnonymous is documented
// as "an extension of NaCl defined by and interoperable with libsodium", which
// is exactly that construction — no cgo, no libsodium, and no hand-rolled
// cryptography (R3).
//
// The plaintext argument is the only value this package ever holds. It is not
// logged, not returned, and not kept: it lives for the length of this call.
func seal(publicKey, plaintext string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil {
		return "", fmt.Errorf("decoding the repository public key: %w", err)
	}

	if len(raw) != keySize {
		return "", fmt.Errorf(
			"the repository public key is %d bytes, want %d", len(raw), keySize)
	}

	var recipient [keySize]byte
	copy(recipient[:], raw)

	sealed, err := box.SealAnonymous(nil, []byte(plaintext), &recipient, rand.Reader)
	if err != nil {
		// The error is returned without the plaintext: an error message is one
		// of the paths FR-054 forbids a value from reaching.
		return "", fmt.Errorf("sealing the value: %w", err)
	}

	return base64.StdEncoding.EncodeToString(sealed), nil
}
