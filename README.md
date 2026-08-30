# forgectl

Check a repository against your forge conventions, and converge it.

forgectl detects the forge hosting a clone's remote, compares the repository
against conventions you declare once, and applies the fixes on request. Nothing
changes without an explicit `apply`, and **no value is ever printed** — not on
stdout, not on stderr, not in a log line, not in an error message.

Three things are checked:

- the **default branch** is the one you declared
- that branch, and your release **tag patterns**, are **protected**
- the **CI variables** your project type needs exist with the right attributes

One variable may be *generated*: a GitLab project access token forgectl creates,
writes straight into the CI variable, and rotates before it expires — so a
release pipeline never depends on a human's personal token.

## Install

```bash
go install github.com/sgaunet/forgectl/cmd/forgectl@latest
```

Or take a binary from the [releases](https://github.com/sgaunet/forgectl/releases).

forgectl runs the `git` binary for local work, so **git must be on `PATH`**.
That is its one runtime dependency; everything else is a single static binary.

## Quick start

With no configuration file at all, against `github.com` or `gitlab.com`:

```bash
export GITLAB_TOKEN=…              # or GITHUB_TOKEN

cd your-repository
forgectl detect                     # what forge is this, and what repository?
forgectl check                      # what has drifted? (read-only)
forgectl apply --yes                # converge it
```

`check` never modifies anything, on the platform or locally. That is enforced by
the import graph rather than by discipline: the package that evaluates
compliance cannot reach the package that executes changes.

## Commands

| Command | What it does |
|---|---|
| `detect` | Print the detected forge and repository |
| `check [PROFILES...]` | Compare against the conventions (read-only) |
| `apply [PROFILES...]` | Apply the fixes, after showing a plan and asking |
| `profiles list` | List every available profile, built-in and configured |
| `profiles show TYPE` | Show one profile: variables, attributes, value sources |
| `version` | Print the version and build information |

`PROFILES` are project-type names, cumulative and deduplicated. When none is
given, the profiles listed in `.forgectl.yaml` at the repository root are used;
when neither supplies any, only the branch and protection checks run and a
warning says CI variables were not checked.

## Exit codes

A calling script can branch on the process outcome alone, with no text parsing:

| Code | Meaning |
|---|---|
| `0` | Succeeded; the repository is compliant |
| `1` | Runtime failure — work began and something broke |
| `2` | Usage error — the invocation or configuration was wrong; nothing was attempted |
| `3` | Succeeded, drift remains |

Where more than one applies, the lowest non-zero code wins: a runtime failure
during a drifted check exits `1`, not `3`.

## Streams

**stdout carries data only** — the detection facts, the compliance report, the
applied-action report, or the profile listing — selectable with
`--output=text|json`.

**stderr carries everything else**: logs, warnings, progress, errors, the plan
preview, and the confirmation prompt. That split is what makes this safe:

```bash
forgectl apply --yes --output=json | jq '.summary'
```

The plan and the prompt are on stderr precisely so that document stays clean.

Colour, spinners, and progress indicators appear only when stdout is a terminal
and `NO_COLOR` is unset.

## Configuration

Precedence is **flags > environment > config file > defaults**.

| Flag | Environment | Default |
|---|---|---|
| `--config PATH` | `FORGECTL_CONFIG` | `~/.config/forgectl/config.yaml` |
| `--remote NAME` | `FORGECTL_REMOTE` | `origin` |
| `--output text\|json` | `FORGECTL_OUTPUT` | `text` |
| `--no-color` | `NO_COLOR` | colour only on a terminal |
| `-v, --verbose` / `--quiet` | `FORGECTL_LOG_LEVEL` | `warn` |

**Credentials come only from the environment**, from the variable each instance
names. They are never read from a flag or from the configuration file, and never
appear in any output.

The configuration file must be mode `0600` or narrower; forgectl refuses to
start otherwise, naming the file, its mode, and the `chmod` that fixes it.
`--allow-insecure-config` bypasses that check and nothing else.

### Example

```yaml
# ~/.config/forgectl/config.yaml  (chmod 0600)

settings:
  default_branch: main

branch_protection:
  enabled: true
  allow_force_push: false
  allow_delete: false
  push_access_level: maintainer   # GitLab only

instances:
  - name: work
    host: git.example.com
    platform: gitlab
    api_url: https://git.example.com/api/v4
    token_env: WORK_GITLAB_TOKEN

# Declared once, referenced by any number of profiles.
values:
  galaxy_api_token: "…"

profiles:
  ansible-role:
    variables:
      - name: GALAXY_API_TOKEN
        value_ref: galaxy_api_token
        masked: true
```

Three profiles are **built into the binary** — `ansible-role`, `go-release`, and
`ssh-deploy` — so forgectl is useful before you write any configuration. A
configured profile of the same name replaces its built-in namesake entirely;
there is no field-level merge, so a partial override cannot silently inherit a
variable you meant to drop.

## A file holding values must not sit in the repository

If a `--var-file`, or any values-bearing file, lies inside the working copy and
git does **not** ignore it, forgectl refuses to run — naming the file and the
`.gitignore` entry that resolves it. That refusal has **no bypass**, not even
`--allow-insecure-config`: such a file is one `git add` away from being
published. The same file, ignored by git, is accepted with no warning.

## Apply

```bash
forgectl apply                     # shows the plan, then asks
forgectl apply --yes               # skip the prompt
forgectl apply --only protection   # branch, protection, vars — comma-separated
forgectl apply --skip vars
forgectl apply --delete-old-branch # remove the old remote branch, once switched
forgectl apply --force-rotate      # rotate generated tokens even without drift
```

Work happens in a fixed order — default branch, then protection including tags,
then variables — because each step depends on the one before it.

`apply` is **idempotent**: on an already-compliant repository the plan is empty,
no confirmation is asked for, and no state-changing call is made. If a run is
interrupted or fails partway, it reports which actions succeeded and which did
not, and rerunning converges from there.

When stdin is not a terminal and `--yes` was not given, `apply` exits `2` rather
than hanging or assuming consent.

## Platform differences forgectl handles for you

- **GitHub tag protection is gone.** The `/tags/protection` API was sunset and
  has returned NULL data since 2024-08-30 — calling it *appears* to succeed
  while protecting nothing. forgectl uses **repository rulesets** for both branch
  and tag protection, and modifies only rulesets it named `forgectl`. A ruleset
  you wrote that grants the required protection still passes: forgectl verifies
  the effect, not its own authorship.
- **GitLab always denies deleting a protected branch**, with no toggle, so
  `allow_delete: false` is satisfied by the branch being protected at all and is
  never reported as drift there.
- **GitHub Actions secrets are write-only.** Their values cannot be read back, so
  they are never compared and are written on every apply, which converges to the
  same result.
- **GitLab refuses to mask** a value that is multiline, contains a space, or is
  shorter than eight characters. forgectl retries the write once unmasked and
  warns, naming the constraint. `protected` is never downgraded.
- **Masking and push access levels have no GitHub equivalent**, and are never
  reported as drift there.

## Development

```bash
mise install       # Go, Task, golangci-lint, goreleaser
task lint          # golangci-lint v2, no findings
task test          # go test -count=2 -race ./...
task vuln          # govulncheck
task build         # CGO_ENABLED=0, -trimpath
```

Tests are black box (`package <pkg>_test`), the forge clients are exercised
against `httptest`, git work runs against real temporary repositories, and one
end-to-end test drives the built binary.

## Licence

See [LICENSE](./LICENSE).
