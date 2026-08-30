package forgetest

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/sgaunet/forgectl/internal/config"
	"github.com/sgaunet/forgectl/internal/forge"
)

// Fake is a hand-written forge.Forge that records every call and answers from
// state the test sets up. It is the whole test double strategy for the
// evaluation and execution layers: no network, no mocking framework, no test
// dependency (R12).
//
// Every field is safe to set directly before use. Calls are recorded in order,
// so a test can assert not only what happened but in which order — which is how
// the rotation sequence of FR-048 is verified.
type Fake struct {
	mu sync.Mutex

	// State the fake answers from.
	Default     string
	Branches    map[string]bool
	Protections map[string]forge.Protection
	Tags        []string
	Variables   map[string]forge.VariableState
	Tokens      []forge.ProjectToken

	// Errors to return instead of an answer, keyed by method name.
	Errors map[string]error

	// Calls records each method invoked, in order.
	Calls []string
	// Writes records every variable write, WITHOUT its value: a test double
	// that stored values would be the one place a value could leak from
	// (FR-054).
	Writes []Write

	// Now is the clock the token lifetime is measured against. A zero Now uses
	// time.Now.
	Now time.Time

	// NextTokenID is the id given to the next created token.
	NextTokenID int
}

// Write is one recorded variable write, carrying the attributes but never the
// value.
type Write struct {
	Name      string
	Secret    bool
	Masked    bool
	Protected bool
	// ValueLen records that a value was supplied without recording what it was,
	// which is enough to assert a write happened.
	ValueLen int
}

// New builds a fake whose default branch is the given one and which knows about
// the given branches.
func New(defaultBranch string, branches ...string) *Fake {
	f := &Fake{
		Default:     defaultBranch,
		Branches:    map[string]bool{},
		Protections: map[string]forge.Protection{},
		Variables:   map[string]forge.VariableState{},
		Errors:      map[string]error{},
		NextTokenID: 1,
	}

	for _, b := range branches {
		f.Branches[b] = true
	}
	if defaultBranch != "" {
		f.Branches[defaultBranch] = true
	}

	return f
}

// WithTokens seeds the fake's project access tokens.
func (f *Fake) WithTokens(tokens ...forge.ProjectToken) *Fake {
	f.Tokens = append(f.Tokens, tokens...)

	return f
}

// noTokens hides the token lifecycle behind the plain Forge interface, so a
// type assertion to forge.TokenIssuer fails. It stands in for GitHub, which has
// no project access token equivalent at all (FR-029).
type noTokens struct{ forge.Forge }

// WithoutTokens returns a view of the fake that does not issue tokens, standing
// in for a GitHub instance.
func WithoutTokens(f *Fake) forge.Forge { return noTokens{f} }

// CallsMade returns the recorded call sequence.
func (f *Fake) CallsMade() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.Calls...)
}

// Mutations returns the calls that change platform state. A check must make
// none of them: it never constructs an executor, and this is how a test proves
// it (FR-031, FR-035).
func (f *Fake) Mutations() []string {
	mutating := map[string]bool{
		"SetDefaultBranch": true, "SetProtection": true, "ProtectTag": true,
		"SetVariable": true, "CreateProjectToken": true, "RevokeProjectToken": true,
	}

	var out []string
	for _, call := range f.CallsMade() {
		if mutating[call] {
			out = append(out, call)
		}
	}

	return out
}

// Clock returns the fake's notion of now, so a test can compute the same
// expiry the code under test will.
func (f *Fake) Clock() time.Time {
	if f.Now.IsZero() {
		return time.Now()
	}

	return f.Now
}

// DefaultBranch reports the branch the fake serves as default.
func (f *Fake) DefaultBranch(_ context.Context) (string, error) {
	if err := f.record("DefaultBranch"); err != nil {
		return "", err
	}

	return f.Default, nil
}

// SetDefaultBranch makes name the fake's default branch.
func (f *Fake) SetDefaultBranch(_ context.Context, name string) error {
	if err := f.record("SetDefaultBranch"); err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.Default = name
	f.Branches[name] = true

	return nil
}

// BranchExists reports whether the fake knows the named branch.
func (f *Fake) BranchExists(_ context.Context, name string) (bool, error) {
	if err := f.record("BranchExists"); err != nil {
		return false, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	return f.Branches[name], nil
}

// Protection reads the protection the fake holds for a branch.
func (f *Fake) Protection(_ context.Context, branch string) (forge.Protection, error) {
	if err := f.record("Protection"); err != nil {
		return forge.Protection{}, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	return f.Protections[branch], nil
}

// SetProtection stores the protection for a branch.
func (f *Fake) SetProtection(_ context.Context, branch string, want forge.Protection) error {
	if err := f.record("SetProtection"); err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	want.Exists = true
	f.Protections[branch] = want

	return nil
}

// TagProtection lists the tag patterns the fake protects.
func (f *Fake) TagProtection(_ context.Context) ([]string, error) {
	if err := f.record("TagProtection"); err != nil {
		return nil, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.Tags...), nil
}

// ProtectTag protects one tag pattern.
func (f *Fake) ProtectTag(_ context.Context, pattern string) error {
	if err := f.record("ProtectTag"); err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.Tags = append(f.Tags, pattern)
	sort.Strings(f.Tags)

	return nil
}

// Variable reads what the fake holds for a CI variable.
func (f *Fake) Variable(_ context.Context, name string, _ bool) (forge.VariableState, error) {
	if err := f.record("Variable"); err != nil {
		return forge.VariableState{}, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	return f.Variables[name], nil
}

// SetVariable records the write and stores the resulting state. The value is
// deliberately not kept: only its length, which is enough to assert the write
// carried one.
func (f *Fake) SetVariable(_ context.Context, write forge.VariableWrite) error {
	if err := f.record("SetVariable"); err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.Writes = append(f.Writes, Write{
		Name:      write.Name,
		Secret:    write.Secret,
		Masked:    write.Masked,
		Protected: write.Protected,
		ValueLen:  len(write.Value),
	})

	f.Variables[write.Name] = forge.VariableState{
		Exists:        true,
		Masked:        write.Masked,
		Protected:     write.Protected,
		Value:         write.Value,
		ValueReadable: true,
	}

	return nil
}

// ProjectTokens lists the active tokens carrying the given name.
func (f *Fake) ProjectTokens(_ context.Context, name string) ([]forge.ProjectToken, error) {
	if err := f.record("ProjectTokens"); err != nil {
		return nil, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	var out []forge.ProjectToken
	for _, tok := range f.Tokens {
		if tok.Name == name && tok.Active && !tok.Revoked {
			out = append(out, tok)
		}
	}

	return out, nil
}

// CreateProjectToken creates a token and returns its value, which a real
// platform discloses exactly once.
func (f *Fake) CreateProjectToken(
	_ context.Context, req forge.ProjectTokenRequest,
) (forge.ProjectToken, string, error) {
	if err := f.record("CreateProjectToken"); err != nil {
		return forge.ProjectToken{}, "", err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	tok := forge.ProjectToken{
		ID:        f.NextTokenID,
		Name:      req.Name,
		ExpiresAt: req.ExpiresAt,
		Active:    true,
	}
	f.NextTokenID++
	f.Tokens = append(f.Tokens, tok)

	return tok, fmt.Sprintf("glpat-fake-%d", tok.ID), nil
}

// RevokeProjectToken revokes one token by id.
func (f *Fake) RevokeProjectToken(_ context.Context, id int) error {
	if err := f.record("RevokeProjectToken"); err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	for i := range f.Tokens {
		if f.Tokens[i].ID == id {
			f.Tokens[i].Active = false
			f.Tokens[i].Revoked = true
		}
	}

	return nil
}

// ActiveTokens counts the tokens of a given name still active, which is the
// invariant a successful apply must leave at exactly one (FR-049).
func (f *Fake) ActiveTokens(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	n := 0
	for _, tok := range f.Tokens {
		if tok.Name == name && tok.Active && !tok.Revoked {
			n++
		}
	}

	return n
}

// Protected is a convenience builder for a fully protected branch.
func Protected(level config.AccessLevel) forge.Protection {
	return forge.Protection{
		Exists:          true,
		AllowForcePush:  false,
		AllowDelete:     false,
		PushAccessLevel: level,
	}
}

// The fake satisfies both interfaces; the GitHub stand-in satisfies only the
// first, which is what makes a generated variable skip there.
var (
	_ forge.Forge       = (*Fake)(nil)
	_ forge.TokenIssuer = (*Fake)(nil)
)

// record notes a call and returns the error the test scripted for it, if any.
func (f *Fake) record(method string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.Calls = append(f.Calls, method)

	return f.Errors[method]
}
