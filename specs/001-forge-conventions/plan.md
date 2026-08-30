# Implementation Plan: Forge Convention Check & Apply

**Branch**: `001-forge-conventions` | **Date**: 2026-08-30 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/001-forge-conventions/spec.md`

**Note**: This template is filled in by the `/speckit-plan` command; its definition describes the execution workflow.

## Summary

forgectl detects the forge hosting a clone's remote, compares the repository against declared
conventions, and converges it — on a check/apply model where nothing changes without an explicit
apply. Three domains are checked: the default branch, branch and tag protection, and the CI
variables a named profile declares. One profile variable may be *generated*: a GitLab project
access token that forgectl creates, writes straight into the CI variable, and rotates before it
expires.

The technical approach is a thin cobra front end over six domain packages that import no CLI code.
A `forge.Forge` interface has exactly two implementations, GitHub and GitLab, each wrapping its
official client but sharing one transport that owns the timeout, the context, and the bounded
retry. Local git work shells out to the `git` binary. Checking and applying are separate packages
over the same plan structure, so `check` is provably read-only: it never constructs an executor.

Two findings from Phase 0 change the spec's stated approach and are carried into the design:
GitHub's tag protection API was sunset in 2024 and silently protects nothing, so tag and branch
protection both go through **repository rulesets**; and GitLab rejects a masked value for three
reasons, not one, so the unmasked retry keys on the class of rejection rather than on multiline
detection.

## Technical Context

**Language/Version**: Go 1.26.1, pinned by the `go` and `toolchain` directives in `go.mod` and by
`mise.toml` (Constitution II)

**Primary Dependencies**: Six direct modules, each proposed to and approved by the author on
2026-08-30 (Constitution: Dependencies & Supply Chain):

| Module | Licence | Purpose |
|---|---|---|
| `github.com/spf13/cobra` | Apache-2.0 | Subcommand parsing |
| `gopkg.in/yaml.v3` | MIT + Apache-2.0 | Config, `.forgectl.yaml`, `--var-file` — see Complexity Tracking |
| `golang.org/x/crypto` | BSD-3 | `nacl/box.SealAnonymous` for GitHub Actions secrets |
| `golang.org/x/term` | BSD-3 | Concealed prompt (FR-044), TTY detection (CLI-004) |
| `github.com/google/go-github/v90` | BSD-3 | GitHub REST client |
| `gitlab.com/gitlab-org/api/client-go` | Apache-2.0 | GitLab REST client (successor to the archived `xanzy/go-gitlab`) |

No test dependency: `testing` and `net/http/httptest` only. No viper or koanf — the precedence
chain is hand-rolled (R14). Local git work uses `os/exec` against the `git` binary, which must be
on `PATH`.

**Storage**: None. Configuration is read from `~/.config/forgectl/config.yaml` at mode 0600; no
state, cache, or credential is ever written by forgectl (FR-050).

**Testing**: `task test` → `go test -count=2 -race ./...`; black-box tests in `package <pkg>_test`;
`httptest` for both forge clients, a hand-written fake `forge.Forge` for the check and apply
layers, real temporary repositories for git work, and one end-to-end test invoking the built binary
(Constitution VII)

**Target Platform**: Single static binary, `CGO_ENABLED=0`, `-trimpath` (Constitution II)

**Project Type**: Single-purpose CLI (Constitution I)

**Performance Goals**: A check against a ten-variable profile completes in under 10 seconds on a
responsive platform (SC-008). Calls within a domain may run concurrently; correctness never depends
on it.

**Constraints**: 30-second per-request timeout, at most 3 retries with exponential backoff, clean
cancellation on `SIGINT`/`SIGTERM` (CLI-005). One run touches exactly one forge instance (FR-032).

**Scale/Scope**: 6 commands, 4 check kinds, 2 platforms, 3 built-in profiles, ~16 distinct
endpoints. Single repository per invocation — batch mode is explicitly out of scope.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Evaluate every gate against `.specify/memory/constitution.md` (v1.0.0). Mark PASS, or record the
violation in Complexity Tracking below with the simpler alternative that was rejected and why.

| Gate | Principle | Status |
|------|-----------|--------|
| One focused job; output usable as a stage in a pipe | I | PASS |
| `CGO_ENABLED=0` static build, `-trimpath`, pinned toolchain; release targets in `.goreleaser.yml` | II | PASS |
| Commands are thin wrappers; business logic in packages importing no CLI package | III | PASS |
| No `utils`/`helpers`/`common`/`base` package; runtime assets embedded with `//go:embed` | III | PASS |
| Concrete types (no generic below 3 concrete impls); errors wrapped with `%w`; `log/slog` only | IV | PASS |
| Stdout carries data only, machine-parseable, selectable with `--output`; stderr carries logs, errors, progress | V | PASS |
| Exit codes 0/1/2 documented in `--help` and code `2` wired explicitly in `main` | V | VIOLATION — a fourth code, `3`, carries "drift found"; author-approved, see Complexity Tracking |
| `NO_COLOR` honoured; no colour, spinners, or progress bars when stdout is not a TTY; `--quiet` and `--verbose` supported | V | PASS |
| Config precedence flags > environment > config file > defaults, stated in `--help` | V | PASS |
| Destructive actions gated by `--yes` or an interactive confirmation | V | PASS |
| Long operations take `context.Context` and cancel on `SIGINT`/`SIGTERM`; every I/O has a timeout; retries bounded and backed off | VI | PASS |
| TDD planned: the failing test precedes the implementation for every behaviour change | VII | PASS |
| Tests in `package <pkg>_test`; internals exposed via `export_test.go`; one end-to-end binary test | VII | PASS |
| Generators declared as Go `tool` deps, invoked via `//go:generate go tool ...`, output committed | VIII | PASS — no generator needed (R13) |
| No new direct dependency, or author approval obtained (MIT/BSD/Apache-2.0, maintained) | Dependencies | VIOLATION — `gopkg.in/yaml.v3` is archived and unmaintained; author-approved, see Complexity Tracking |
| No credentials or tokens in the repository, logs, or error messages | Dependencies | PASS |

**Post-design re-check (after Phase 1)**: unchanged. The design introduces no further deviation.
Two specific risks were checked and cleared:

- *Interfaces are not generics*. `forge.Forge` has two implementations, which is polymorphism, not
  the generic that Constitution IV defers until three concrete types exist. No type parameter
  appears anywhere in the design.
- *No catch-all package*. Every package is named for its domain. The shared HTTP transport lives in
  `internal/forge`, whose domain it serves, rather than in a general-purpose HTTP package.

## Project Structure

### Documentation (this feature)

```text
specs/001-forge-conventions/
├── plan.md              # This file (/speckit-plan command output)
├── research.md          # Phase 0 output (/speckit-plan command)
├── data-model.md        # Phase 1 output (/speckit-plan command)
├── quickstart.md        # Phase 1 output (/speckit-plan command)
├── contracts/           # Phase 1 output (/speckit-plan command)
│   ├── cli.md               # Commands, flags, exit codes, stream split
│   ├── config.schema.json   # config.yaml, .forgectl.yaml, --var-file
│   ├── output.schema.json   # --output=json, shared by check and apply
│   └── forge-endpoints.md   # Every endpoint used, per platform
├── checklists/
│   └── requirements.md  # Spec quality checklist (complete)
└── tasks.md             # Phase 2 output (/speckit-tasks command - NOT created by /speckit-plan)
```

### Source Code (repository root)

```text
cmd/forgectl/
├── main.go                    # cobra root, config precedence wiring, signal-cancelled context,
│                              # exit-code classification (0 / 1 / 2 / 3)
├── detect.go                  # detect
├── check.go                   # check [TYPES...]
├── apply.go                   # apply [TYPES...]
├── profiles.go                # profiles list | profiles show TYPE
├── version.go                 # version, from runtime/debug.ReadBuildInfo
├── export_test.go             # exposes the exit-code classifier to package main_test
└── main_test.go               # package main_test: end-to-end against the built binary

internal/gitrepo/              # local working copy: discovery, remote URL parsing, git commands
├── gitrepo.go                 # rev-parse, remote get-url, branch -m, push -u, set-head, --delete
├── remoteurl.go               # SSH and HTTPS forms with optional port -> host, owner, repo
├── ignore.go                  # check-ignore, backing FR-056
├── export_test.go
└── *_test.go                  # package gitrepo_test, against real temp repositories

internal/config/               # schema, load, validate, merge, profile selection
├── config.go                  # types and the four-layer precedence merge (R14)
├── validate.go                # FR-009..FR-018 rules, all raised before any platform call
├── permissions.go             # 0600 enforcement (FR-007) and the in-repo refusal (FR-056)
├── builtin.go                 # //go:embed builtin_profiles.yaml
├── builtin_profiles.yaml      # ansible-role, go-release, ssh-deploy
└── *_test.go

internal/forge/                # platform abstraction and shared transport
├── forge.go                   # the Forge interface and its domain types
├── transport.go               # 30s timeout, bounded backoff retry, context propagation
├── errors.go                  # ErrUnknownHost, ErrNoCredential, ErrInsufficientRights
├── github/                    # go-github-backed: repo, rulesets, Actions secrets and variables
│   ├── github.go
│   ├── seal.go                # nacl/box.SealAnonymous
│   └── *_test.go              # httptest
└── gitlab/                    # client-go-backed: project, protected branches and tags,
    ├── gitlab.go              # CI variables, project access tokens
    ├── token.go               # the §7 lifecycle
    └── *_test.go              # httptest

internal/values/               # value resolution chain: --var-file > config > generator > prompt
internal/compliance/           # the check catalog, evaluation, and plan construction — read-only
internal/apply/                # plan execution, domain ordering, partial-failure reporting
internal/report/               # text and JSON renderers over the shared result schema
```

**Structure Decision**: Commands live in `cmd/forgectl` and do nothing but parse, validate, call,
and format. Every decision lives in a domain package that imports no CLI code, which is what lets
the whole check catalog be tested against a fake forge with no terminal and no network.

The split between `internal/compliance` and `internal/apply` is the load-bearing one: compliance
evaluates state and *builds* a plan, apply is the only package that *executes* one. `check` never
imports `internal/apply`, so FR-031's "MUST NOT modify any state" is enforced by the import graph
rather than by discipline — a compile-time guarantee that a reviewer can verify at a glance.

`internal/forge` holds the interface and the shared transport; the two platform packages import it,
never each other. Adding Forgejo later means one new subpackage and no change to compliance, apply,
or report.

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| Exit code `3` for "drift found", beyond the 0/1/2 that Constitution V enumerates | `check` must let a CI caller distinguish "the repository drifted" from "the tool broke". Collapsing them onto `1` makes a drifted repository indistinguishable from a network failure, which defeats SC-006. | Reassigning `1` to drift and `2` to runtime failure — the spec's original scheme — was rejected because it redefines the two codes Constitution V fixes, needing a MAJOR amendment, and would leave a usage error sharing code `2` with auth and network failures. Author chose to keep the constitution's semantics intact and extend rather than redefine. Decided 2026-08-30. |
| `gopkg.in/yaml.v3` is archived (2025-04-01) and its author declares it unmaintained, failing the "actively maintained" rule | The config, `.forgectl.yaml`, and `--var-file` are YAML, and the standard library cannot parse YAML at all. | `github.com/goccy/go-yaml` (MIT, active, written expressly to replace the archived project) was recommended and declined by the author. A JSON config, needing no dependency, was also declined: it would cost comments and turn the multiline SSH key of §4.1 into an escaped one-liner. Exposure is bounded — the parser only ever reads owner-only files, never network input — and `govulncheck` in CI is the standing guard. Decided 2026-08-30. |
