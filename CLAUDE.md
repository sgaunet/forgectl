# CLAUDE.md

This file provides guidance to Claude Code when working with this repository.

## Operating Guidelines

**Read `docs/operating-guidelines.md` at the start of every session.** It
defines how to plan, verify, and iterate in this repository: plan mode,
subagent strategy, verification gates, self-improvement loop, and the
communication contract. Treat it as load-bearing context.

## Behavioral Guidelines

Behavioral guidelines to reduce common LLM coding mistakes. Merge with project-specific instructions as needed.

**Tradeoff:** These guidelines bias toward caution over speed. For trivial tasks, use judgment.

### 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

### 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

### 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it - don't delete it.

When your changes create orphans:
- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

### 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:
- "Add validation" → "Write tests for invalid inputs, then make them pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan:
```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

## Repository Overview

forgectl is a Go CLI that detects the forge hosting a clone's remote (GitHub or
GitLab), compares the repository against conventions declared once in
`~/.config/forgectl/config.yaml`, and converges it on explicit `apply`. It
checks the default branch, branch and tag protection, and CI variables — one of
which it can generate and rotate (a GitLab project access token).

**`.specify/memory/constitution.md` is binding and supersedes assistant
defaults.** When a case is unaddressed there, ask rather than guess.

## Architecture

- **Thin command layer**: `cmd/forgectl/` only parses, validates, calls, and
  formats. Every decision lives under `internal/`, which imports no CLI code.
- **Read/write split by type**: `forge.Reader` and `forge.Writer`
  (`internal/forge/forge.go:21`). `internal/compliance` is handed only a
  `Reader`, so `check` cannot mutate anything.
- **Import-graph guard**: `internal/compliance` must never reach
  `internal/apply` or `internal/values`; `internal/compliance/imports_test.go`
  fails the build if it does.
- **Values never surface**: a resolved value goes to exactly one
  `SetVariable` call — never logged, never wrapped into an error, never
  written to disk, never sent to `internal/report`.
- **Domain packages only**: `apply`, `compliance`, `config`, `forge`,
  `gitrepo`, `report`, `values`. `utils`, `helpers`, `common`, `base` are
  forbidden. Each has a `doc.go` stating its contract — read it first.
- See `docs/architecture.md` for detailed design decisions.

## Development Commands

```bash
mise install       # Go 1.26.7, task, golangci-lint, goreleaser (pinned)
task lint          # go generate ./... && golangci-lint run
task test          # go test -count=2 -race ./...
task build         # CGO_ENABLED=0 go build -trimpath -o forgectl ./cmd/forgectl
task vuln          # govulncheck ./...
task coverage      # coverage percentage, cmd/ excluded
task snapshot      # goreleaser snapshot build
```

A change is complete only when `lint`, `test`, `vuln`, and a clean
`go generate ./...` diff all pass.

## Code Quality Standards

**Linters configured** (do not duplicate rules):
- golangci-lint v2: see `.golangci.yml` (`default: all`, ~30 disabled)
- pre-commit: see `.pre-commit-config.yaml` (runs test, lint, build)

**Key conventions:**
- **TDD, non-negotiable**: the failing test lands with, or before, the change.
- **Black box tests**: `package <pkg>_test`. Internals a test needs are exposed
  through `export_test.go` in the package — never by moving the test in.
- Wrap errors with `fmt.Errorf("...: %w", err)`; classify on the sentinels in
  `internal/forge/errors.go`. Never flatten an error into a string.
- **stdout is data only** (`--output=text|json`); stderr carries logs, the plan
  preview, prompts, and errors. Exit codes: `0` compliant, `1` runtime failure,
  `2` usage error, `3` drift — lowest non-zero wins.
- Every I/O call takes a `context.Context` first and is bounded
  (`internal/forge/transport.go`). Logging is `log/slog` only.
- Prefer concrete types; no generic before three concrete implementations.
- Embed runtime assets with `//go:embed`; commit generated code.

## File Locations

- **Source**: `cmd/forgectl/` (CLI), `internal/` (domain packages)
- **Tests**: alongside source, `*_test.go`, `package <pkg>_test`
- **Specs**: `specs/001-forge-conventions/` (spec, plan, tasks, contracts)
- **Constitution**: `.specify/memory/constitution.md`
- **Config**: `Taskfile.yml`, `mise.toml`, `.golangci.yml`, `.goreleaser.yaml`
- **CI**: `.github/workflows/` (lint, test, snapshot on main; release on `v*`)
- **Docs**: `docs/`

## Documentation

- `docs/architecture.md`: system design and component overview
- `docs/workflows.md`: development process, gates, and release flow
- `docs/patterns.md`: code patterns and conventions
- `docs/operating-guidelines.md`: session workflow and verification rules
