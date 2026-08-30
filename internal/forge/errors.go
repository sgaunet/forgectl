package forge

import (
	"errors"

	"github.com/sgaunet/forgectl/internal/config"
)

var (
	// ErrUnknownHost reports a remote host matching no instance. It is an alias
	// of the config sentinel so a caller can classify it without importing both
	// packages (FR-003, R11).
	ErrUnknownHost = config.ErrUnknownHost

	// ErrNoCredential reports an instance whose credential environment variable
	// is unset or empty, raised before any network call (FR-005, R11).
	ErrNoCredential = config.ErrNoCredential

	// ErrInsufficientRights reports a credential that may not perform the
	// operation. It makes the affected check SKIP with that reason rather than
	// fail, so a token without token-management rights does not turn a healthy
	// repository into a drifted one (FR-030).
	ErrInsufficientRights = errors.New("the credential lacks the rights for this operation")

	// ErrNotSupported reports an operation the platform has no equivalent for.
	// A generated variable on GitHub raises it, and is skipped with a warning
	// rather than failing the run (FR-029).
	ErrNotSupported = errors.New("not supported on this platform")

	// ErrTokenLifetime reports a requested token lifetime above the instance
	// maximum. Its message carries the platform's own wording, which states the
	// permitted maximum (FR-052).
	ErrTokenLifetime = errors.New("the requested token lifetime exceeds the instance maximum")

	// ErrMaskRejected reports a value the platform refuses to mask. It drives
	// the single unmasked retry of FR-043, and its message names the constraint
	// — never the value (R7).
	ErrMaskRejected = errors.New("the platform refuses to mask this value")
)
