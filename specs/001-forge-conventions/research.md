# Phase 0 Research: Forge Convention Check & Apply

**Feature**: `specs/001-forge-conventions` · **Date**: 2026-08-30

Every unknown carried into planning is resolved below. Findings that contradict the source
specification are marked **CORRECTION** — the source specification was written against APIs that
have since changed.

---

## R1 — Toolchain

**Decision**: Go 1.26.1, pinned by the `go` and `toolchain` directives in `go.mod` and by
`mise.toml`. golangci-lint v2.12.1, goreleaser v2, Task, git 2.55.

**Rationale**: All present on the development host and verified. Go 1.26 satisfies the Go 1.24+
floor that Constitution VIII requires for the `tool` directive.

**Alternatives considered**: Pinning to an older minor for wider builder compatibility — rejected,
since goreleaser builds inside a pinned toolchain anyway and the constitution requires a rebuilt
tag to produce the same binary.

**Amended during implementation (2026-08-30)**: the pin moved from Go 1.26.1 to **1.26.7**.
`govulncheck ./...` reported 13 standard-library vulnerabilities in 1.26.1 — `net/http`,
`crypto/tls`, `crypto/x509`, `net/url`, and others — all fixed by 1.26.6. The constitution requires
that gate to pass in CI, and a patch release changes no language version, so the pin was raised in
both `go.mod` and `mise.toml`. Under 1.26.7 govulncheck reports zero vulnerabilities the code
calls. One uncalled advisory remains, `GO-2026-5932` against `golang.org/x/crypto/openpgp`: it
lives in a module forgectl requires for `nacl/box` and imports no part of.

---

## R2 — GitHub tag protection is gone; rulesets replace it

**CORRECTION to spec §6.3**, which says "GitHub: tag protection rules API".

**Decision**: Protect tag patterns on GitHub through **repository rulesets**, not the tag
protection API.

- Create: `POST /repos/{owner}/{repo}/rulesets`
- Body: `target: "tag"`, `enforcement: "active"`,
  `conditions.ref_name.include: ["refs/tags/v*"]`,
  `rules: [{"type": "deletion"}, {"type": "non_fast_forward"}]`
- Read back: `GET /repos/{owner}/{repo}/rulesets` and match on the ruleset name forgectl owns.

**Rationale**: GitHub's May 2024 sunset notice announced that tag protection rules would be
migrated to tag rulesets, and that the tag protection REST and GraphQL endpoints would begin
returning NULL data after **2024-08-30**. Calling the old endpoint today succeeds while silently
protecting nothing — the worst possible failure mode for a tool whose whole purpose is verifying
that protection exists.

The two rule types verified against GitHub's REST rules documentation: `deletion` restricts
deletion, `non_fast_forward` blocks force pushes. `target` accepts `branch`, `tag`, and `push`.
The same two rule types express branch protection, so one ruleset code path serves both the
`protection` and `tags` checks.

**Consequence for the spec**: The Assumptions entry "Protected tags on GitHub" is resolved —
protected tags ARE supported on GitHub, through a different mechanism than the source
specification named. No check needs to degrade to a skip.

**Alternatives considered**: Classic branch protection (`/repos/{o}/{r}/branches/{b}/protection`)
for branches plus rulesets for tags — rejected, two mechanisms where one suffices, and rulesets are
where GitHub is moving. Note the classic branch protection API still works and remains an option if
ruleset semantics prove awkward for the `push_access_level` analogue (see R9).

**Sources**: [Sunset Notice — Tag Protections](https://github.blog/changelog/2024-05-29-sunset-notice-tag-protections/),
[REST API endpoints for rules](https://docs.github.com/en/rest/repos/rules)

---

## R3 — GitHub Actions secret encryption

**Decision**: `golang.org/x/crypto/nacl/box.SealAnonymous`.

Signature verified as present:

```go
func SealAnonymous(out, message []byte, recipient *[32]byte, rand io.Reader) ([]byte, error)
```

The package documents anonymous sealing as "an extension of NaCl defined by and interoperable with
libsodium" — exactly the sealed box GitHub requires.

**Flow**: `GET /repos/{o}/{r}/actions/secrets/public-key` returns a base64 key and its `key_id`;
decode to `[32]byte`, seal, base64-encode the ciphertext, then
`PUT /repos/{o}/{r}/actions/secrets/{name}` with `encrypted_value` and `key_id`.

**Rationale**: No cgo, no libsodium, interoperable by construction. Encryption is never hand-rolled.

**Alternatives considered**: A third-party sealed-box wrapper — rejected, `x/crypto` is the
canonical source and is already approved.

> An initial research pass reported that `nacl/box` has no `SealAnonymous`. That was wrong and was
> corrected by reading the package documentation directly.

---

## R4 — YAML parsing

**Decision**: `gopkg.in/yaml.v3`, chosen by the author over the recommended alternative.

**Constitutional exception, recorded**: `github.com/go-yaml/yaml` was **archived on 2025-04-01**,
and its README carries a section headed "THIS PROJECT IS UNMAINTAINED" in which the author states
they will not hand off maintenance. This fails the constitution's "actively maintained"
requirement in Dependencies & Supply Chain. The conflict was raised with the author, who chose
`yaml.v3` regardless. It is logged in the plan's Complexity Tracking table.

**Operational consequence**: `yaml.v3` will receive no security fix. `govulncheck ./...` in CI is
the only mechanical guard, and the parser only ever reads files the user owns at mode 0600 — never
network input — which bounds the exposure.

**Alternatives considered**: `github.com/goccy/go-yaml` (MIT, active, written as a replacement for
the archived project) — recommended and declined. A JSON config, which needs no dependency at all —
declined, it would cost comments and turn the multiline SSH key of §4.1 into an escaped one-liner.

**Note**: `yaml.v3` also removes any need for viper or koanf. The flags > environment > config file
> defaults chain of CLI-004 is a four-layer merge of roughly fifty lines, and hand-rolling it keeps
the precedence explicitly testable rather than inherited from a framework's own rules.

---

## R5 — Forge API clients

**Decision**: Official client libraries, chosen by the author.

- GitHub: `github.com/google/go-github/v90` (BSD-3-Clause). Confirm the exact latest major at
  `go get` time; v90 was current at research.
- GitLab: `gitlab.com/gitlab-org/api/client-go` (Apache-2.0).

**Rationale**: Both are permissively licensed and actively maintained. `github.com/xanzy/go-gitlab`
was **archived on 2024-12-10** and development moved to the GitLab-hosted module above — anything
still importing `xanzy` is importing a dead path, so the new path is the only acceptable one.
Pagination, error shapes, and rate-limit headers come for free, which matters most for the GitLab
project access token lifecycle, the fiddliest part of the tool.

**Constitution VI compliance**: Both clients accept a caller-supplied `*http.Client` and take a
`context.Context` on every call. forgectl injects one `*http.Client` carrying the explicit
30-second timeout of CLI-005, wrapped in a `http.RoundTripper` that implements the bounded,
backed-off retry. That transport lives in `internal/forge`, is shared by both platforms, and is
where rate-limit handling is tested — the clients never get to define the retry policy.

**Alternatives considered**: Hand-rolled `net/http` against the ~16 endpoints actually used —
recommended for its far smaller audit surface, and declined.

**Sources**: [go-gitlab migration announcement](https://github.com/xanzy/go-gitlab/issues/2060),
[gitlab-org/api/client-go](https://gitlab.com/gitlab-org/api/client-go)

---

## R6 — Local git operations

**Decision**: Shell out to the `git` binary through `os/exec`, chosen by the author.

Commands used, each mapping to a step of spec §6.1 and FR-056:

| Purpose | Command |
|---|---|
| Find the working copy | `git rev-parse --show-toplevel` |
| Read the remote URL | `git remote get-url <remote>` |
| Detect no-commit state | `git rev-parse --verify HEAD` |
| Rename the local branch | `git branch -m master main` |
| Create from the remote branch | `git branch main origin/master` |
| Push with tracking | `git push -u origin main` |
| Update the local remote head | `git remote set-head origin main` |
| Delete the old remote branch | `git push origin --delete master` |
| Is a file git-ignored (FR-056) | `git check-ignore -q <path>` |

**Rationale**: The spec's own procedure is written as git commands, so behaviour matches what the
maintainer would type. Credential helpers, SSH agent forwarding, and `url.*.insteadOf` rewrites are
inherited rather than reimplemented — the single largest hidden cost of the alternative.

**Consequence**: `git` must be on `PATH`. Its absence is a usage error (exit 2), detected at
startup with a message naming the requirement. This is the one runtime dependency the static binary
does not eliminate, and it is worth stating in `--help`.

**Alternatives considered**: `github.com/go-git/go-git/v5` — a genuinely self-contained binary, but
a large dependency whose push authentication would have to be rebuilt from scratch.

---

## R7 — GitLab masked variable constraints

**Decision**: Treat every masking rejection, not only the multiline case, as the trigger for the
unmasked retry of FR-043.

GitLab enforces on a masked value:

- single line — a multiline value is rejected with `Value must be a single line.`
- no spaces
- at least 8 characters

**CORRECTION to spec §6.4**, which frames the retry as specific to multiline values and to
instance version. All three constraints produce the same class of rejection, and the SSH private
key of the `ssh-deploy` profile fails the first two. Implementing the retry against the
"multiline" condition alone would leave a short or space-bearing value failing hard.

**Implementation**: attempt the write with the configured `masked` flag; on a 400 whose message
identifies a masking constraint, retry once with `masked: false` and emit a warning on stderr
naming the constraint. Never retry more than once, and never silently downgrade `protected`.

---

## R8 — GitLab project access token lifecycle

**Decision**: Implement the §7 lifecycle against `/projects/:id/access_tokens`.

| Operation | Call |
|---|---|
| List | `GET /projects/:id/access_tokens` — returns id, name, scopes, expires_at, active, revoked |
| Create | `POST /projects/:id/access_tokens` — body `name`, `scopes`, `access_level`, `expires_at` |
| Revoke | `DELETE /projects/:id/access_tokens/:token_id` |

- `expires_at` is `YYYY-MM-DD`. forgectl always sends it explicitly, computed as today +
  `expires_in`, rather than relying on the instance default.
- The token value is returned **only** in the creation response. This is what makes FR-050 and
  FR-051 necessary: there is no way to recover it, so the CI variable write must follow immediately
  and a failure between the two is unrecoverable except by rotating again.
- Instance maximum lifetime: 365 days by default since GitLab 17.6, administrator-configurable.
  Exceeding it returns a validation error reading `The expiration date must be within the allowed
  lifetime.` — surfaced verbatim to satisfy FR-052.
- `access_level`: 30 Developer, 40 Maintainer (60 Owner).

**Rationale**: Confirms every §7 assumption. The default `expires_in: 180d` sits comfortably inside
the 365-day ceiling, so the common path never trips the limit.

**Sources**: [Project access tokens API](https://docs.gitlab.com/api/project_access_tokens/)

---

## R9 — GitLab protection semantics

**Decision**:

- Branches: `POST/PATCH /projects/:id/protected_branches` with `name`, `allow_force_push`,
  `push_access_level`. Access levels: `0` none, `30` developer, `40` maintainer, `60` admin — the
  mapping the config's `push_access_level` string resolves to.
- **Deletion of a protected branch is always denied by GitLab**; there is no toggle. The
  `allow_delete: false` setting of §4.1 is therefore a no-op on GitLab and MUST be reported as
  satisfied whenever the branch is protected at all, never as drift.
- Tags: `POST /projects/:id/protected_tags` with `name` as the pattern (wildcards accepted).

**Consequence**: The per-platform mapping table of FR-026 gains one row — `allow_delete` is
inherent on GitLab and explicit on GitHub (`deletion` rule).

---

## R10 — Terminal handling

**Decision**: `golang.org/x/term` for both `ReadPassword` (the concealed prompt of FR-044) and
`IsTerminal` (the TTY detection of CLI-004).

**Rationale**: A no-echo prompt cannot be written without termios syscalls, which is precisely what
this package exists to encapsulate. Since it is approved for that, using its `IsTerminal` too
avoids a second, hand-rolled way of asking the same question.

**Alternatives considered**: `os.Stat` plus `ModeCharDevice` for TTY detection — correct and
dependency-free, but it would leave two different terminal checks in one binary.

---

## R11 — Exit code classification

**Decision**: Domain packages export sentinel errors; `cmd/forgectl` owns the single classification
switch that maps an error to an exit code.

- Exit `2` — the invocation or configuration was wrong and nothing was attempted:
  `config.ErrInvalid`, `config.ErrPermissions`, `config.ErrValuesInRepo`, `gitrepo.ErrNotARepo`,
  `gitrepo.ErrNoCommits`, `gitrepo.ErrNoRemote`, `gitrepo.ErrGitMissing`,
  `forge.ErrUnknownHost`, `forge.ErrNoCredential`, plus cobra's own flag errors.
- Exit `1` — work began and something broke: everything else.
- Exit `3` — success with drift outstanding, carried as a value on the result, not as an error.
- Exit `0` — success, compliant.

**Rationale**: Constitution IV requires `%w` wrapping precisely so callers can use `errors.Is`, and
this is the caller that needs it. Keeping the switch in `main` keeps the CLI contract in the CLI
layer, where Constitution III says it belongs, while the classification itself stays testable —
`export_test.go` exposes it to a `package main_test` unit test, and the end-to-end binary test
covers all four codes for real.

**Alternatives considered**: An `internal/exit` package holding the mapping — rejected, it would
pull a CLI concern into a domain package for no testability gain.

---

## R12 — Testing strategy

**Decision**:

- **Forge clients**: `net/http/httptest` servers returning recorded payload shapes; both official
  clients accept a base URL, so they point at the test server. Covers pagination, the masking
  rejection of R7, the lifetime error of R8, and rate-limit retry.
- **Compliance and apply**: a hand-written fake implementing the `forge.Forge` interface, in the
  test package. No network, no mocking framework.
- **gitrepo**: real temporary repositories built by running `git init` in `t.TempDir()`, with a
  bare repository standing in for the remote so push, `set-head`, and `--delete` are exercised for
  real.
- **End-to-end**: `cmd/forgectl` built by the test, run against a temporary repository plus an
  `httptest` forge, asserting exit code, the stdout/stderr split, and that no known value appears
  in either stream.
- **Confidentiality (SC-003)**: one table-driven test seeds recognisable sentinel values through
  every path and asserts they appear in no stream, no log line, and no error string.

**Rationale**: Everything above is `testing` and `net/http/httptest` from the standard library. No
test dependency is requested, which keeps the approved dependency list at six.

---

## R13 — Code generation

**Decision**: No generator for v1. Enum-like types (`CheckStatus`, `Platform`, `ValueSource`) get
hand-written `String()` methods — three types, a handful of values each.

**Rationale**: Constitution VIII governs generators that exist; introducing `stringer` for three
small enums would add a tool dependency and a committed-output obligation for no benefit. If the
count grows, `go get -tool golang.org/x/tools/cmd/stringer` and a `//go:generate go tool stringer`
directive next to each file is the sanctioned route.

**Note**: The built-in profiles are embedded with `//go:embed`, per Constitution III. Embedding is
not code generation and needs no directive.

---

## R14 — Configuration precedence

**Decision**: Hand-rolled four-layer merge in `internal/config`, resolved in this order:
defaults → config file → environment → flags, with each layer overwriting only the fields it sets.

**Rationale**: CLI-004 fixes the precedence and Constitution V requires it to be stated in
`--help`. A hand-rolled merge makes each layer boundary a test case, which is what "stated and
testable" demands. Both viper and koanf were dropped along with the YAML question in R4; neither is
needed once the merge is explicit.

**Implementation note**: Flags must be distinguishable between "unset" and "set to the zero value",
so the merge reads cobra's `Changed` flag rather than comparing against zero.

---

## Open items carried into implementation

1. **`go-github` major version** — v90 at research time. Confirm the current major on first
   `go get`; the import path carries it.
2. **GitHub ruleset ownership** — forgectl must recognise the rulesets it created versus ones the
   maintainer wrote by hand. Decision: match on a fixed ruleset name (`forgectl`), the same flat
   naming rule §7 already applies to tokens, and never modify a ruleset carrying another name.
   Recorded in the data model as `Ruleset.Name`.
