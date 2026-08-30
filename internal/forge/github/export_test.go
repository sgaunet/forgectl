package github

// This file exposes the internals a package github_test needs, so the tests
// stay black box without those internals becoming public API
// (Constitution VII).

// Seal exposes the sealed-box helper to the test package. Sealing is the one
// piece of cryptography in forgectl, and it is worth verifying end to end —
// seal, then open with the private key — rather than only through the client
// that calls it.
func Seal(publicKey, plaintext string) (string, error) { return seal(publicKey, plaintext) }
