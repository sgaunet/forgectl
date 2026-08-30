# Architecture

## System Overview

forgectl is a single static Go binary with a thin cobra command layer over
seven domain packages. One run follows a fixed pipeline: discover the local git
repository, parse its remote to identify the forge, load and validate the
declared conventions, evaluate compliance against the platform (read-only),
and — only for `apply` — execute the resulting plan in a fixed domain order.

The central architectural property is that **`check` cannot mutate anything**,
and that is enforced twice: by the type system (the evaluation layer is handed
a `forge.Reader`, which has no write method) and by the import graph
(`internal/compliance/imports_test.go` fails if `compliance` can reach
`internal/apply` or `internal/values`).

## Components

- **`cmd/forgectl/`** — cobra commands (`detect`, `check`, `apply`,
  `profiles`, `version`), flag/env/file precedence wiring, signal handling, and
  the exit-code classifier in `main.go`. Parses, validates, calls, formats;
  nothing more.
- **`internal/config`** — loads, merges, and validates declared intent: the
  config file, per-repository profile selection (`.forgectl.yaml`), and the
  three profiles built into the binary (`ansible-role`, `go-release`,
  `ssh-deploy`). Every rule is enforced before any platform call.
- **`internal/gitrepo`** — the local working copy: discovery from any
  subdirectory, remote URL parsing, `.gitignore` lookups, branch commands.
  Shells out to the `git` binary, which must be on `PATH`.
- **`internal/forge`** — the platform abstraction. Owns the `Reader`/`Writer`/
  `Forge`/`TokenIssuer` interfaces, the shared domain types, the bounded HTTP
  transport, and the sentinel errors callers classify on. Implementations live
  in `forge/github` and `forge/gitlab`; `forge/forgetest` holds the fake.
- **`internal/compliance`** — evaluates the repository against the conventions
  and builds the plan that would converge it. Read-only by construction.
- **`internal/apply`** — the only package that executes a plan. Owns the fixed
  domain ordering, partial-failure reporting, and token rotation.
- **`internal/values`** — resolves a variable's value through the ordered chain
  of override file, configuration, generator, and concealed prompt.
- **`internal/report`** — renders results to stdout in text and JSON over one
  shared schema. No value ever reaches this package.

## Design Decisions

1. **Read/write split as two interfaces** (`internal/forge/forge.go:21`).
   Making `check`'s inability to mutate a property of the type signature means
   no future edit in `compliance` can make it write, without a reviewer having
   to notice.
2. **Import-graph test as a structural guard.** A comment saying "do not import
   apply here" is discipline; `imports_test.go` is a check.
3. **Values resolved one at a time, never returned in an error.** The resolver
   hands out a single value per call rather than a map a caller could hold, so
   there is no collection of sensitive values to accidentally log.
4. **stdout/stderr split.** The plan preview and confirmation prompt go to
   stderr precisely so `apply --output=json | jq` stays a clean document.
5. **Platform quirks absorbed in the forge layer.** GitHub tag protection is
   sunset, so both branch and tag protection use repository rulesets; GitHub
   Actions credentials are write-only, so they are written every apply; GitLab
   refuses to mask some values, so the write retries unmasked and warns.
   Callers above `forge` do not branch on platform.
6. **Concrete types over generics.** A generic is introduced only after three
   concrete implementations of the same shape exist.

## Integration Points

- **GitHub API** via `github.com/google/go-github/v90` — repository rulesets
  for branch and tag protection, Actions variables and encrypted values.
- **GitLab API** via `gitlab.com/gitlab-org/api/client-go` — protected branches
  and tags, CI/CD variables, project access tokens.
- **`git` binary** — the one runtime dependency, invoked by `internal/gitrepo`.
- **Credentials** come only from the environment variable each instance names
  (`token_env`); never from a flag, never from the config file.

## Data Flow

```
gitrepo.Discover → remote URL → config.Instance (host match)
    → forge.New (github | gitlab)
    → compliance.Evaluate(Reader, conventions) → Plan
        ├─ check:  report.Render(Plan)                       → exit 0 | 3
        └─ apply:  plan preview (stderr) → confirm
                   → apply.Run(Writer, Plan, values.Resolver)
                   → report.Render(actions)                  → exit 0 | 1
```

Work in `apply` happens in a fixed order — default branch, then protection
including tags, then variables — because each step depends on the one before.
`apply` is idempotent: on a compliant repository the plan is empty, nothing is
asked, and no state-changing call is made.

## Bounds

Every platform call takes a `context.Context` and is cancellable on `SIGINT`
and `SIGTERM`. A single request is bounded at 30s (`RequestTimeout`), retries
are bounded and back off from 500ms (`internal/forge/transport.go`).
