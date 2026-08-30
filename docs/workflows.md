# Development Workflows

## Setup

```bash
mise install                  # Go 1.26.7, task, golangci-lint, goreleaser, syft
task dev:install-pre-commit   # pre-commit install
```

The `task` default target refuses to run until the pre-commit hook is
installed. Tool versions are pinned in `mise.toml`; do not rely on globally
installed binaries.

## Feature Development

The order of work is fixed by Constitution VII and is not negotiable:

1. Branch from `main`.
2. **Write the failing test first.** A behaviour change arrives with the test
   that fails without it. Implementation submitted without that test is sent
   back.
3. Implement until the test passes, then refactor.
4. Run the gates locally (below).
5. Open a PR; CI runs lint and test on every pull request.

Changes that touch a spec live under `specs/001-forge-conventions/`. A
`/speckit-plan` run must clear its Constitution Check gate before Phase 0
research and again after Phase 1 design; surviving violations go in the plan's
Complexity Tracking table with the rejected simpler alternative.

## Quality Gates

A change is complete only when all four pass locally **and** in CI:

| Gate | Command | Requirement |
|---|---|---|
| Lint | `task lint` | golangci-lint v2, no findings |
| Test | `task test` | `go test -count=2 -race ./...`, no failures |
| Vuln | `task vuln` | `govulncheck ./...`, nothing known |
| Generate | `go generate ./...` | no diff against committed files |

`task check-before-commit` runs test, snapshot, and lint together.

Disagreements about what is idiomatic are settled by editing `.golangci.yml`,
not by arguing in review.

## Testing Strategy

- **Black box only.** Tests live in `package <pkg>_test`. Internals a test
  needs are exposed through an `export_test.go` inside the package — never by
  moving the test back in.
- **`-count=2`** defeats result caching and surfaces state leaking between
  runs. **`-race`** is the only mechanical check for data races. Neither flag
  is optional.
- Forge clients are exercised against `httptest`; `internal/forge/forgetest`
  provides the fake `Forge` for higher layers.
- Git work runs against real temporary repositories, not mocks.
- Argument parsing, the four exit codes, and the stdout/stderr split each have
  tests, and at least one test drives the built binary end to end.
- `internal/compliance/imports_test.go` guards the import graph. If it fails,
  the fix is the import, not the test.

Coverage is reported by `task coverage`, with `cmd/` excluded from the profile.

## Dependencies

Adding a **direct** module dependency requires the author's prior approval. A
new dependency must be MIT, BSD, or Apache-2.0 and actively maintained; the
standard library is the first choice. Dependabot watches `gomod`, `docker`, and
`github-actions` monthly.

## CI/CD

GitHub Actions, all jobs on `ubuntu-latest` via `jdx/mise-action`:

| Workflow | Trigger | Runs |
|---|---|---|
| `linter.yml` | push to `main`, PR | `task lint` |
| `test.yml` | push to `main`, PR | `task test`, `task vuln` |
| `snapshot.yml` | push to `main` | `task snapshot` |
| `release.yml` | tag `v*` | `task release` |

## Release Process

Releases are cut with `task release` and nothing else — goreleaser v2, from a
`v*` tag. Build targets live in `.goreleaser.yaml`, never in ad-hoc commands.
Every release publishes SHA-256 checksums and an SBOM (syft). Binaries are
`CGO_ENABLED=0` and `-trimpath`, so anyone can rebuild a tag and get the same
artifact.
