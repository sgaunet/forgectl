package forge

import (
	"context"
	"time"

	"github.com/sgaunet/forgectl/internal/config"
)

// Reader is the read-only half of the platform abstraction: everything the
// compliance layer needs to judge a repository, and nothing that could change
// it.
//
// internal/compliance accepts a Reader and never a Forge. FR-031's "check MUST
// NOT modify any local or platform state" is therefore enforced by the type
// system: the evaluation layer is handed no method that writes, so no amount of
// future editing there can make check mutate anything.
//
// Every method takes a context first, so every call is cancellable and bounded
// (Constitution VI).
type Reader interface {
	// DefaultBranch reports the branch the platform serves as default (FR-023).
	DefaultBranch(ctx context.Context) (string, error)
	// BranchExists reports whether the named branch is on the platform. It is
	// what lets the protection check skip with a reason rather than fail when
	// the target branch is not there yet (FR-024).
	BranchExists(ctx context.Context, name string) (bool, error)
	// Protection reads the protection in force on a branch (FR-024).
	Protection(ctx context.Context, branch string) (Protection, error)
	// TagProtection lists the tag patterns the platform protects (FR-025).
	TagProtection(ctx context.Context) ([]string, error)
	// Variable reads what the platform reports about a CI variable (FR-026).
	Variable(ctx context.Context, name string, secret bool) (VariableState, error)
}

// Writer is the half that changes things. Only internal/apply is ever given
// one.
type Writer interface {
	// SetDefaultBranch makes name the platform default (FR-037, FR-038).
	SetDefaultBranch(ctx context.Context, name string) error
	// SetProtection puts the wanted protection in force on a branch (FR-037).
	SetProtection(ctx context.Context, branch string, want Protection) error
	// ProtectTag protects one tag pattern (FR-025).
	ProtectTag(ctx context.Context, pattern string) error
	// SetVariable creates or updates a CI variable (FR-042).
	//
	// This is the only method that receives a value. It writes it to the
	// platform and nowhere else: the value is never logged, never wrapped into
	// an error, and never returned (FR-050, FR-054).
	SetVariable(ctx context.Context, write VariableWrite) error
}

// Forge is both halves: what apply is given, and what each platform package
// implements in full.
type Forge interface {
	Reader
	Writer
}

// TokenIssuer is the part of the platform that issues project-scoped access
// tokens. Only GitLab implements it; a Forge that does not is what makes a
// generated variable skip with a warning rather than fail (FR-029).
type TokenIssuer interface {
	// ProjectTokens lists the active tokens carrying the given name (FR-028).
	ProjectTokens(ctx context.Context, name string) ([]ProjectToken, error)
	// CreateProjectToken creates a token and returns it with its value, which
	// the platform discloses exactly once (R8).
	//
	// The second return is that value. It goes straight into SetVariable and is
	// never stored anywhere else (FR-050).
	CreateProjectToken(ctx context.Context, req ProjectTokenRequest) (ProjectToken, string, error)
	// RevokeProjectToken revokes one token by id (FR-048).
	RevokeProjectToken(ctx context.Context, id int) error
}

// Protection is the state of a protected branch, expressed in the terms both
// platforms can be mapped onto.
type Protection struct {
	// Exists is false when the branch carries no protection at all.
	Exists bool
	// AllowForcePush is GitHub's absent non_fast_forward rule, and GitLab's
	// allow_force_push flag.
	AllowForcePush bool
	// AllowDelete is GitHub's absent deletion rule. On GitLab it is ALWAYS
	// false: deleting a protected branch is denied with no toggle (R9), so a
	// configured allow_delete: false is satisfied by protection existing at
	// all and must never be reported as drift.
	AllowDelete bool
	// PushAccessLevel is GitLab only. It carries the zero value on GitHub,
	// which models no equivalent, and is therefore never compared there
	// (FR-026).
	PushAccessLevel config.AccessLevel
}

// VariableState is what the platform reports about a CI variable.
type VariableState struct {
	Exists bool
	// Masked is GitLab only.
	Masked bool
	// Protected is GitLab only.
	Protected bool
	// Value is GitLab only, and always empty on GitHub, whose Actions
	// credentials are write-only.
	Value string
	// ValueReadable is false wherever the platform cannot disclose a value.
	//
	// It is an explicit field rather than an inference from the platform, so
	// the comparison in internal/compliance reads as a fact about the value
	// rather than a special case about one platform (FR-027).
	ValueReadable bool
}

// VariableWrite is one variable write, carrying the only value that ever
// crosses this package boundary.
type VariableWrite struct {
	Name  string
	Value string

	// Secret selects the Actions credential store over the Actions variable
	// store on GitHub. It has no effect on GitLab, where every variable is a
	// CI variable (FR-026).
	Secret bool
	// Masked and Protected are GitLab attributes with no GitHub equivalent.
	Masked    bool
	Protected bool
}

// ProjectToken is one project access token as the platform reports it.
type ProjectToken struct {
	ID        int
	Name      string
	ExpiresAt time.Time
	Active    bool
	Revoked   bool
}

// DaysRemaining reports how many whole days are left before the token expires,
// counted from now. A token that has already expired reports zero.
func (t ProjectToken) DaysRemaining(now time.Time) int {
	if t.ExpiresAt.IsZero() {
		return 0
	}

	days := int(t.ExpiresAt.Sub(now).Hours() / 24)
	if days < 0 {
		return 0
	}

	return days
}

// ProjectTokenRequest is the token forgectl asks the platform to create.
type ProjectTokenRequest struct {
	Name      string
	Scopes    []string
	Role      config.AccessLevel
	ExpiresAt time.Time
}

// Target is the single instance a run operates against, together with the
// credential that reaches it. One run touches exactly one instance (FR-032).
type Target struct {
	Instance config.Instance
	Owner    string
	Repo     string

	// Credential is the token read from the environment variable the instance
	// names. It is never logged and never rendered (FR-054, CLI-004).
	Credential string
}

// Project renders the repository as owner/name, the identifier both platforms
// accept.
func (t Target) Project() string { return t.Owner + "/" + t.Repo }

// Resolve matches a remote host against the configured instances, falling back
// to the built-in definitions, and reads the credential from the environment
// variable that instance names.
//
// Both failures happen before any network call: an unmatched host wraps
// ErrUnknownHost (FR-003) and a missing credential wraps ErrNoCredential
// (FR-005). Neither message ever carries the credential.
func Resolve(cfg *config.Config, host, owner, repo string) (Target, error) {
	inst, err := config.ResolveInstance(cfg, host)
	if err != nil {
		return Target{}, err
	}

	credential, err := config.Credential(inst)
	if err != nil {
		return Target{}, err
	}

	return Target{Instance: inst, Owner: owner, Repo: repo, Credential: credential}, nil
}
