# Contract: Command-Line Interface

**Feature**: `specs/001-forge-conventions` · **Date**: 2026-08-30

This is the stable surface. Constitution I makes composability and a stable output contract the
tie-breaker against every internal concern, so a change here is a breaking change.

---

## Commands

```text
forgectl [command]

Commands:
  detect                Print the detected forge and repository
  check   [TYPES...]    Compare repository state against conventions (read-only)
  apply   [TYPES...]    Apply fixes
  profiles list         List available profiles
  profiles show TYPE    Show one profile in detail
  version               Print version and build info
```

`TYPES` are positional profile names, cumulative and deduplicated by variable name. Precedence:
command-line arguments, else `.forgectl.yaml` at the repository root, else none (FR-017). With no
profile selected, only `branch` and `protection` run and a warning goes to stderr (FR-019).

## Global flags

| Flag | Environment | Default | Purpose |
|---|---|---|---|
| `--config PATH` | `FORGECTL_CONFIG` | `~/.config/forgectl/config.yaml` | Config file |
| `--remote NAME` | `FORGECTL_REMOTE` | `origin` | Remote to inspect |
| `--output text\|json` | `FORGECTL_OUTPUT` | `text` | Output format |
| `--json` | — | — | Alias for `--output=json` |
| `--no-color` | `NO_COLOR` | colour only on a TTY | Disable colour |
| `--allow-insecure-config` | — | `false` | Bypass the 0600 permission check only |
| `-v, --verbose` | `FORGECTL_LOG_LEVEL` | `warn` | Verbose logs on stderr |
| `--quiet` | `FORGECTL_LOG_LEVEL` | — | Errors only on stderr |

Precedence is **flags > environment > config file > defaults**, stated in `--help` (CLI-004).
`--verbose` and `--quiet` together is a usage error.

`--allow-insecure-config` bypasses the file-permission refusal of FR-007 and nothing else. It does
**not** bypass the in-repository refusal of FR-056.

## `apply` flags

| Flag | Purpose |
|---|---|
| `--yes`, `-y` | Skip the confirmation prompt |
| `--delete-old-branch` | Delete the old remote branch after the default branch is switched |
| `--var-file PATH` | YAML file overriding variable values, one-off and per-repository |
| `--only DOMAIN` | Restrict to `branch`, `protection`, `vars` (comma-separated) |
| `--skip DOMAIN` | Exclude the named domains (comma-separated) |
| `--force-rotate` | Rotate generated tokens even without drift |

`--only` and `--skip` together is a usage error (FR-036). The `tags` work belongs to the
`protection` domain.

## Stream split (CLI-001)

**stdout** carries data only — the detection facts, the compliance report, the applied-action
report, the profile listing. Nothing else is ever written there.

**stderr** carries logs (`log/slog`), warnings, progress, errors, the plan preview, and the
confirmation prompt.

The plan preview and prompt are on stderr deliberately: `forgectl apply --yes --output=json | jq`
must yield a clean document, and a prompt written to stdout would corrupt it.

Colour, spinners, and progress indicators are emitted only when stdout is a TTY and `NO_COLOR` is
unset (CLI-004).

## Exit codes (CLI-002)

| Code | Meaning |
|---|---|
| `0` | Succeeded; the repository is compliant |
| `1` | Runtime failure — work began and something broke: network, authentication rejected, a platform error, a git command failing, an apply that completed only in part |
| `2` | Usage error — the invocation or configuration was wrong and nothing was attempted |
| `3` | Succeeded, drift remains — `check` when any check fails, `apply` when it finishes leaving drift it declared not auto-fixable |

Where more than one applies, the lowest non-zero code wins: a runtime failure during a drifted
check exits `1`, not `3`.

Exit `2` covers: an unknown flag or an unusable flag combination, an unknown profile name, invalid
configuration, a config file whose permissions are too wide, a values-bearing file that git does not
ignore, a working directory outside a git working copy, a repository with no commits, an unknown
remote, a host matching no instance, an unset credential variable, and `git` missing from `PATH`.

Cobra sets no exit code of its own, so all four are wired explicitly in `main` (Constitution V).

## Confirmation (CLI-003)

`apply` prints its plan, then prompts, unless `--yes` is given or the plan is empty. Actions
requiring confirmation: renaming and pushing the default branch, changing the platform default
branch, deleting the old remote branch, changing branch or tag protection, overwriting an existing
CI variable, creating and revoking project access tokens.

`detect`, `check`, `profiles`, and `version` are read-only and never prompt.

When stdin is not a TTY and `--yes` was not given, `apply` exits `2` rather than hanging or
assuming consent.

## Output

`--output=text` renders the layouts of spec §9. `--output=json` emits one document per run,
conforming to [`output.schema.json`](./output.schema.json), identical in shape for `check` and
`apply`.

**No value ever appears in either format**, nor in a log line, a progress message, or an error
string. Only statuses: `missing`, `differs`, `expires in N days` (FR-054).

## Cancellation (CLI-005)

`SIGINT` and `SIGTERM` cancel the root context. Every platform call carries a 30-second timeout and
at most 3 retries with exponential backoff. An interrupted `apply` reports what completed; rerunning
converges. An interruption between creating a token and writing its variable is reported as such,
because that token cannot be recovered (FR-051).
