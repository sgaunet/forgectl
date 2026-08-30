# Code Patterns & Best Practices

## Package Contracts Live in `doc.go`

Every `internal/` package carries a `doc.go` stating what it owns and what it
must never do. Read it before editing the package — it is the contract, not
decoration.

```go
// internal/report/doc.go
// Package report renders results to stdout in text and JSON over one shared
// schema. No value ever reaches this package.
package report
```

## Error Handling

Errors are wrapped, never flattened, so `errors.Is` and `errors.As` still work
up the stack:

```go
if err != nil {
    return fmt.Errorf("read protection for %s: %w", branch, err)
}
```

Callers classify on the sentinels in `internal/forge/errors.go`:

```go
// internal/forge/errors.go:23
ErrInsufficientRights = errors.New("the credential lacks the rights for this operation")
ErrNotSupported       = errors.New("not supported on this platform")
ErrTokenLifetime      = errors.New("the requested token lifetime exceeds the instance maximum")
ErrMaskRejected       = errors.New("the platform refuses to mask this value")
```

`ErrNotSupported` is what turns a platform gap into a skip-with-warning rather
than a failure — a generated variable on GitHub, masking on GitHub, tag
protection deletion on GitLab.

Two sentinels in `cmd/forgectl/main.go` carry outcome rather than failure:
`errDrift` (exit 3, carries no message because the report already said
everything) and `errUsage` (exit 2, wraps cobra's own flag and argument
errors). A command returns one or the other, never both — that is what
implements "the lowest non-zero code wins".

## Values Never Surface

A resolved value has exactly one destination: `Writer.SetVariable`. It is never
logged, never wrapped into an error, never returned, never written to disk, and
never reaches `internal/report`.

The resolver enforces this by shape, not by convention: it hands out one value
per call rather than a map a caller could hold and later print.

When adding code that touches a value, the checks are: does it appear in a
`%v`/`%s`? in an `fmt.Errorf`? in an `slog` attribute? If yes, that is the bug.

## Interface Segregation Enforces Read-Only

```go
// internal/forge/forge.go:21
type Reader interface { /* DefaultBranch, BranchExists, Protection, ... */ }
type Writer interface { /* SetDefaultBranch, SetProtection, SetVariable, ... */ }
type Forge  interface { Reader; Writer }
```

`internal/compliance` accepts a `Reader`. `internal/apply` accepts a `Forge`.
The evaluation layer is handed no method that writes, so no future edit there
can make `check` mutate anything.

`TokenIssuer` is separate again: only GitLab implements it, and a type
assertion failure is what makes a generated variable skip with a warning.

## Testing Patterns

- **File naming**: `foo.go` / `foo_test.go`, same directory.
- **Package**: `package <pkg>_test`, always. Never `package <pkg>` in a test.
- **Escape hatch**: `export_test.go` inside the package exposes internals a
  black box test needs (`internal/forge/github/export_test.go`,
  `cmd/forgectl/export_test.go`).
- **Fakes over mocks**: `internal/forge/forgetest` provides a hand-written fake
  `Forge`. Forge clients themselves are tested against `httptest` servers.
- **Real git**: `internal/gitrepo` tests run against temporary repositories
  created with the actual `git` binary.
- **Structural tests**: `internal/compliance/imports_test.go` shells out to
  `go list -deps` and fails if a forbidden package appears in the transitive
  import list.
- Always run with `-count=2 -race`.

## Context and Bounded I/O

Every method that performs I/O takes `ctx context.Context` first. `main` wires
`signal.NotifyContext` for `SIGINT` and `SIGTERM`, so any run is safe to
interrupt.

`internal/forge/transport.go` bounds the rest: a 30s timeout per request
(retries excluded) and exponential backoff from 500ms over a bounded retry
count. Do not add an unbounded loop or a call without a timeout.

## Go Conventions Specific to This Repository

- **No `utils`, `helpers`, `common`, or `base` packages.** Packages are named
  for the domain they serve.
- **Concrete types first.** A generic is introduced only once three concrete
  implementations of the same shape exist.
- **`log/slog` only**, at a level the user controls (`--quiet`, `-v`,
  `FORGECTL_LOG_LEVEL`), recording only what the reader can act on.
- **Runtime assets are embedded** with `//go:embed`; the binary never depends
  on files sitting next to it.
- **Generated code is committed.** Generators are Go tool dependencies invoked
  by `//go:generate go tool ...`; `go generate ./...` must produce no diff.
- **Serialised shapes are contracts.** JSON output is fixed by
  `specs/001-forge-conventions/contracts/output.schema.json` and YAML config by
  `config.schema.json`. Both are snake_case — `tagliatelle` is configured to
  that convention rather than the contract being rewritten.
