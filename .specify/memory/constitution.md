<!--
Sync Impact Report
==================
Version change: (template, unversioned) → 1.0.0
Bump rationale: First concrete ratification. Every placeholder token replaced with binding
rules; no prior version existed to be backward-incompatible with.

Principles defined (template slot → final name):
- [PRINCIPLE_1_NAME] → I. Single Purpose, Composable
- [PRINCIPLE_2_NAME] → II. Static, Reproducible Delivery
- [PRINCIPLE_3_NAME] → III. Thin Commands, Domain Packages
- [PRINCIPLE_4_NAME] → IV. Concrete Types, Wrapped Errors, Actionable Logs
- [PRINCIPLE_5_NAME] → V. Stable CLI Contract (NON-NEGOTIABLE)
- (added) → VI. Interruptible, Bounded I/O
- (added) → VII. Test-First, Black Box (NON-NEGOTIABLE)
- (added) → VIII. Generated Code Is Committed

Sections:
- [SECTION_2_NAME] → Dependencies & Supply Chain
- [SECTION_3_NAME] → Development Workflow & Quality Gates
- Governance → filled ([GOVERNANCE_RULES] replaced)

Removed sections: none.

Templates requiring updates:
- ✅ .specify/templates/plan-template.md (Constitution Check gates made concrete; Go CLI
     source layout replaces Python/web placeholder tree)
- ✅ .specify/templates/tasks-template.md (tests promoted from OPTIONAL to MANDATORY per
     Principle VII; Go path conventions; principle-driven task types added)
- ✅ .specify/templates/spec-template.md (CLI contract requirements added to Requirements
     section per Principle V)
- ✅ .specify/templates/checklist-template.md (reviewed, no constitution-driven change needed)
- ✅ .claude/skills/speckit-*/SKILL.md (reviewed; agent-neutral, no stale references)
- ⚠ README.md (does not exist yet; must document exit codes, --output, config precedence
     when created)

Follow-up TODOs: none. No placeholder tokens deferred.
-->

# forgectl Constitution

## Core Principles

### I. Single Purpose, Composable

forgectl MUST do one focused job. Every binary this repository ships MUST have a single
responsibility and MUST be usable as one stage of a shell pipeline rather than as an
interactive application. Work that does not serve that one job belongs in another tool, not
in another flag.

**Rationale**: A tool with one job has an output contract small enough to keep stable, and a
tool that composes can be combined by its users in ways its author never planned.

### II. Static, Reproducible Delivery

- The binary MUST build with `CGO_ENABLED=0` and link statically. A default build is not
  static: `net` and `os/user` link cgo unless it is disabled.
- Builds MUST pass `-trimpath` and MUST use the toolchain pinned by the `go` and `toolchain`
  directives in `go.mod` and by `mise.toml`.
- Releases MUST go through goreleaser v2 (`task release`) and MUST publish SHA-256 checksums
  and an SBOM. Build targets MUST live in `.goreleaser.yml`, never in ad-hoc commands.

**Rationale**: A static binary runs on any host with no runtime dependency, and a pinned,
trimmed build lets anyone rebuild a tag and obtain the same binary.

### III. Thin Commands, Domain Packages

- Command code MUST be a thin wrapper — parse, validate, call, format — built on cobra, or on
  the stdlib `flag` package when the tool has no subcommands.
- Business logic MUST live in packages that import no CLI package, so it stays testable
  without a terminal.
- Packages MUST be named for the domain they serve. `utils`, `helpers`, `common`, and `base`
  packages are FORBIDDEN.
- Runtime assets (templates, default configs, static files) MUST be embedded with `//go:embed`.

**Rationale**: Catch-all packages attract unrelated code and grow into import cycles.
Embedded assets mean the binary never depends on files sitting next to it.

### IV. Concrete Types, Wrapped Errors, Actionable Logs

- Code MUST prefer concrete types. A generic MUST NOT be introduced before the same concrete
  implementation exists for three or more types, because generics cost readability and most
  Go code never needs them.
- Errors MUST be wrapped with `fmt.Errorf("...: %w", err)` and MUST NOT be flattened into a
  string, so callers can still use `errors.Is` and `errors.As`.
- Logging MUST use `log/slog` at a level the user controls, and MUST record only what the
  reader can act on.
- `task lint` (golangci-lint v2) MUST pass. "Idiomatic" means whatever the linter accepts;
  disagreements are settled by editing `.golangci.yml`, not by arguing in review.

### V. Stable CLI Contract (NON-NEGOTIABLE)

- Stdout MUST carry data only, machine-parseable, selectable with `--output=text|json`.
  Stderr MUST carry logs, errors, and progress.
- Exit codes MUST be documented in `--help` and covered by a test: `0` success, `1` runtime
  failure, `2` usage error. Cobra sets no exit code of its own — `Execute()` returns an error
  and the scaffolded `main` exits `1` for everything — so code `2` MUST be wired explicitly
  in `main`.
- The tool MUST respect `NO_COLOR`, MUST NOT emit colour, spinners, or progress bars when
  stdout is not a TTY, and MUST support `--quiet` and `--verbose`.
- Configuration precedence MUST be flags > environment > config file > defaults, and MUST be
  stated in `--help`.
- Destructive actions MUST require `--yes` or an interactive confirmation, so a mistyped
  command cannot delete anything.

**Rationale**: The stdout/stderr split is what makes the tool safe inside a pipe, and a
documented exit code is the only thing a calling script can branch on.

### VI. Interruptible, Bounded I/O

- Every long-running operation MUST take a `context.Context` and MUST cancel cleanly on
  `SIGINT` and `SIGTERM`, so the tool is always safe to interrupt.
- All I/O MUST have an explicit timeout. Retries MUST be bounded and MUST back off.

**Rationale**: An operation that cannot be cancelled or timed out will eventually hang a
script that has no way to recover.

### VII. Test-First, Black Box (NON-NEGOTIABLE)

- Development MUST follow TDD: write the failing test, make it pass, then refactor. A
  behaviour change MUST arrive with the test that fails without it.
- Tests MUST live in `package <pkg>_test` (black box), so they exercise the API a consumer
  actually has. Internals a test needs MUST be exposed through an `export_test.go` inside the
  package, never by moving the test back into the package.
- `task test` (`go test -count=2 -race ./...`) MUST pass. `-count=2` defeats result caching
  and surfaces state leaking between runs; `-race` is the only mechanical check for data
  races.
- Argument parsing, exit codes, and the stdout/stderr split MUST each be tested, and at least
  one test MUST invoke the built binary end to end.

### VIII. Generated Code Is Committed

- Generators MUST be declared as Go tool dependencies (`go get -tool <module>`, the Go 1.24+
  `tool` directive) rather than installed globally, so every checkout runs the same versions.
- Each generator MUST be invoked by a `//go:generate go tool <name> ...` directive placed next
  to the file it produces, and the whole tree MUST regenerate with `go generate ./...`.
- Generated files MUST be committed, so a clean checkout builds without running any generator.

## Dependencies & Supply Chain

- The standard library MUST be the first choice. Adding a direct module dependency MUST be
  proposed to the author and approved before it lands, so the dependency surface stays small
  enough for one person to audit.
- A new dependency MUST be MIT, BSD, or Apache-2.0 licensed and actively maintained. Copyleft
  and unlicensed modules are refused.
- `govulncheck ./...` MUST pass in CI, and Dependabot MUST watch `gomod`, `docker`, and
  `github-actions` monthly.
- Secrets MUST NOT appear in the repository, in logs, or in error messages. Configuration
  comes from the environment.

## Development Workflow & Quality Gates

- The order of work for any change is fixed by Principle VII: failing test, implementation,
  refactor. Implementation submitted without the test that fails without it MUST be sent back.
- A change is complete only when all four gates pass locally and in CI:
  1. `task lint` — golangci-lint v2, no findings.
  2. `task test` — `go test -count=2 -race ./...`, no failures.
  3. `govulncheck ./...` — no known vulnerabilities.
  4. `go generate ./...` — produces no diff against committed files.
- A change that adds a direct module dependency MUST carry the author's prior approval, per
  Dependencies & Supply Chain.
- Releases are cut with `task release` and nothing else.
- Every `/speckit-plan` run MUST complete its Constitution Check gate before Phase 0 research
  and again after Phase 1 design. Violations that survive design MUST be recorded in the
  plan's Complexity Tracking table with the simpler alternative that was rejected and why.

## Governance

- This constitution supersedes the assistant's defaults and any convention not written here.
  A spec, plan, or patch that violates a principle MUST be revised to comply, or the conflict
  MUST be raised to the author with the trade-off stated.
- When two principles conflict, composability and a stable output contract win: breaking a
  pipe is worse than an awkward internal design.
- When a case is genuinely unaddressed here, the assistant MUST ask rather than guess, and
  SHOULD choose the simpler design, with fewer dependencies and more tests.
- Amendments MUST be made by editing this file in a commit that states what changed and why,
  and MUST bump the version below. Versioning is semantic: MAJOR for a principle removed or
  redefined incompatibly, MINOR for a principle added or guidance materially expanded, PATCH
  for clarifications and wording that change no rule.
- An amendment MUST propagate to `.specify/templates/plan-template.md`,
  `.specify/templates/spec-template.md`, and `.specify/templates/tasks-template.md` in the
  same commit, and the Sync Impact Report at the top of this file MUST be updated to record it.

**Version**: 1.0.0 | **Ratified**: 2026-08-30 | **Last Amended**: 2026-08-30
