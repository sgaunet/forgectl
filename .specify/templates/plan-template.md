# Implementation Plan: [FEATURE]

**Branch**: `[###-feature-name]` | **Date**: [DATE] | **Spec**: [link]

**Input**: Feature specification from `/specs/[###-feature-name]/spec.md`

**Note**: This template is filled in by the `/speckit-plan` command; its definition describes the execution workflow.

## Summary

[Extract from feature spec: primary requirement + technical approach from research]

## Technical Context

<!--
  ACTION REQUIRED: Fill in the values marked NEEDS CLARIFICATION. The pre-filled entries are
  fixed by the constitution and MUST NOT be changed here; changing them is a constitution
  amendment.
-->

**Language/Version**: Go, pinned by the `go` and `toolchain` directives in `go.mod` and by
`mise.toml` (Constitution II)

**Primary Dependencies**: Standard library by default. Any direct module dependency listed here
MUST already be approved by the author and be MIT/BSD/Apache-2.0 licensed (Constitution:
Dependencies & Supply Chain)

**Storage**: [if applicable, e.g., local config file, SQLite, files or N/A]

**Testing**: `task test` -> `go test -count=2 -race ./...`; black-box tests in
`package <pkg>_test`; at least one end-to-end test invoking the built binary (Constitution VII)

**Target Platform**: Single static binary, `CGO_ENABLED=0`, `-trimpath` (Constitution II)

**Project Type**: Single-purpose CLI (Constitution I)

**Performance Goals**: [domain-specific, e.g., 10k lines/sec or NEEDS CLARIFICATION]

**Constraints**: [domain-specific, e.g., <200ms p95, explicit I/O timeouts, offline-capable or
NEEDS CLARIFICATION]

**Scale/Scope**: [domain-specific, e.g., number of subcommands, objects handled or NEEDS
CLARIFICATION]

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Evaluate every gate against `.specify/memory/constitution.md` (v1.0.0). Mark PASS, or record the
violation in Complexity Tracking below with the simpler alternative that was rejected and why.

| Gate | Principle | Status |
|------|-----------|--------|
| One focused job; output usable as a stage in a pipe | I | [PASS / VIOLATION] |
| `CGO_ENABLED=0` static build, `-trimpath`, pinned toolchain; release targets in `.goreleaser.yml` | II | [PASS / VIOLATION] |
| Commands are thin wrappers; business logic in packages importing no CLI package | III | [PASS / VIOLATION] |
| No `utils`/`helpers`/`common`/`base` package; runtime assets embedded with `//go:embed` | III | [PASS / VIOLATION] |
| Concrete types (no generic below 3 concrete impls); errors wrapped with `%w`; `log/slog` only | IV | [PASS / VIOLATION] |
| Stdout carries data only, machine-parseable, selectable with `--output`; stderr carries logs, errors, progress | V | [PASS / VIOLATION] |
| Exit codes 0/1/2 documented in `--help` and code `2` wired explicitly in `main` | V | [PASS / VIOLATION] |
| `NO_COLOR` honoured; no colour, spinners, or progress bars when stdout is not a TTY; `--quiet` and `--verbose` supported | V | [PASS / VIOLATION] |
| Config precedence flags > environment > config file > defaults, stated in `--help` | V | [PASS / VIOLATION] |
| Destructive actions gated by `--yes` or an interactive confirmation | V | [PASS / VIOLATION] |
| Long operations take `context.Context` and cancel on `SIGINT`/`SIGTERM`; every I/O has a timeout; retries bounded and backed off | VI | [PASS / VIOLATION] |
| TDD planned: the failing test precedes the implementation for every behaviour change | VII | [PASS / VIOLATION] |
| Tests in `package <pkg>_test`; internals exposed via `export_test.go`; one end-to-end binary test | VII | [PASS / VIOLATION] |
| Generators declared as Go `tool` deps, invoked via `//go:generate go tool ...`, output committed | VIII | [PASS / VIOLATION] |
| No new direct dependency, or author approval obtained (MIT/BSD/Apache-2.0, maintained) | Dependencies | [PASS / VIOLATION] |
| No credentials or tokens in the repository, logs, or error messages | Dependencies | [PASS / VIOLATION] |

## Project Structure

### Documentation (this feature)

```text
specs/[###-feature]/
├── plan.md              # This file (/speckit-plan command output)
├── research.md          # Phase 0 output (/speckit-plan command)
├── data-model.md        # Phase 1 output (/speckit-plan command)
├── quickstart.md        # Phase 1 output (/speckit-plan command)
├── contracts/           # Phase 1 output (/speckit-plan command)
└── tasks.md             # Phase 2 output (/speckit-tasks command - NOT created by /speckit-plan)
```

### Source Code (repository root)

<!--
  ACTION REQUIRED: Replace `<domain>` with the concrete packages this feature adds. The shape
  below is fixed by Constitution III: commands stay thin, business logic lives in domain-named
  packages that import no CLI package.
-->

```text
cmd/forgectl/
└── main.go                    # cobra/flag wiring, config precedence, signal handling,
                               # exit-code mapping (0 success, 1 runtime failure, 2 usage error)

internal/<domain>/             # business logic; MUST NOT import cmd/ or any CLI package
├── <domain>.go
├── <domain>_test.go           # package <domain>_test (black box)
├── export_test.go             # only when a black-box test needs an internal
└── testdata/                  # golden files for output-contract tests

internal/<domain>/assets/      # only if the feature ships runtime assets, embedded via //go:embed
```

**Structure Decision**: [Name the concrete packages added or touched, and state which package
holds the logic that the command wrapper calls]

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| [e.g., a generic with only 2 concrete impls] | [current need] | [why concrete types insufficient] |
| [e.g., a new direct dependency] | [specific problem] | [why the standard library insufficient] |
