# Phase 1 Data Model: Forge Convention Check & Apply

**Feature**: `specs/001-forge-conventions` · **Date**: 2026-08-30

Types are grouped by the package that owns them. Every validation rule cites the requirement it
enforces. All validation runs before any platform call (FR-016).

---

## `internal/config` — declared intent

### Config

The whole configuration tree, after the four-layer merge of R14.

| Field | Type | Notes |
|---|---|---|
| `Instances` | `[]Instance` | Declared forge endpoints |
| `Settings` | `Settings` | Global conventions |
| `Values` | `map[string]string` | The shared value store |
| `BranchProtection` | `BranchProtection` | Defaults applied to the target branch |
| `Profiles` | `map[string]Profile` | Built-ins, overridden and extended by the file |
| `Path` | `string` | Where it was loaded from, for error messages |

**Validation**

- The file's mode MUST be no wider than `0600` unless bypassed (FR-007).
- Instance names MUST be unique; hosts MUST be unique.
- Every `value_ref` MUST resolve against `Values` (FR-010).
- Every duration MUST match `^[0-9]+d$` (FR-013).

### Instance

| Field | Type | Default | Notes |
|---|---|---|---|
| `Name` | `string` | — | Identifier used in output |
| `Host` | `string` | — | Matched against the parsed remote host |
| `Platform` | `Platform` | — | `github` or `gitlab` |
| `APIURL` | `string` | derived from `Host` | Base URL handed to the client |
| `TokenEnv` | `string` | — | Name of the environment variable holding the credential |

Two instances are built in and used when the host matches nothing configured (FR-003):
`github.com` → platform `github`, `TokenEnv: GITHUB_TOKEN`; `gitlab.com` → platform `gitlab`,
`TokenEnv: GITLAB_TOKEN`.

**Validation**: `Platform` MUST be one of the two known values. `TokenEnv` MUST name a set,
non-empty variable at resolution time, or `forge.ErrNoCredential` is raised before any network call
(FR-005).

### Settings

| Field | Type | Default |
|---|---|---|
| `DefaultBranch` | `string` | `main` (FR-015) |

### BranchProtection

| Field | Type | Default | Platform |
|---|---|---|---|
| `Enabled` | `bool` | `true` | both |
| `AllowForcePush` | `bool` | `false` | both |
| `AllowDelete` | `bool` | `false` | GitHub explicit; **inherent on GitLab** (R9) |
| `PushAccessLevel` | `AccessLevel` | `maintainer` | GitLab only; ignored on GitHub (FR-026) |

`AccessLevel` is `none` \| `developer` \| `maintainer`, mapping to GitLab `0` \| `30` \| `40`.

### Profile

| Field | Type | Default |
|---|---|---|
| `Name` | `string` | — |
| `Variables` | `[]VariableDefinition` | — |
| `ProtectedTags` | `[]string` | `[]` (FR-014) |

Three are built into the binary, embedded with `//go:embed` (Constitution III): `ansible-role`,
`go-release`, `ssh-deploy`. A configured profile of the same name replaces the built-in one
entirely — there is no field-level merge, so a partial override cannot silently inherit a variable
the maintainer meant to drop (FR-008).

### VariableDefinition

| Field | Type | Default | Notes |
|---|---|---|---|
| `Name` | `string` | — | The CI variable key |
| `Value` | `string` | — | Inline literal — one of three sources |
| `ValueRef` | `string` | — | Key into `Config.Values` — one of three sources |
| `Generator` | `*Generator` | — | Generated value — one of three sources |
| `Secret` | `bool` | `true` | GitHub: Actions secret vs Actions variable |
| `Masked` | `bool` | `false` | GitLab only |
| `Protected` | `bool` | `false` | GitLab only |

**Validation**

- Exactly one of `Value`, `ValueRef`, `Generator` MUST be set; zero or more than one is an error
  naming the variable (FR-009).
- A variable definition MUST NOT carry a platform or instance field — one run targets one instance
  (FR-032).

**Union across profiles** (FR-018): variables are keyed by `Name`. Two profiles may declare the
same name only if every field above is identical; any difference — attribute or value source — is a
configuration error naming the variable and both profiles.

### Generator

Present only for `generator: gitlab-pat`.

| Field | Type | Default |
|---|---|---|
| `Kind` | `string` | `gitlab-pat` (the only kind in v1) |
| `TokenName` | `string` | `forgectl` |
| `Scopes` | `[]string` | `["api"]` |
| `Role` | `AccessLevel` | `maintainer` |
| `ExpiresIn` | `Days` | `180d` |
| `RotateBefore` | `Days` | `60d` |
| `RevokeRotated` | `bool` | `true` |

**Validation**: `RotateBefore` MUST be less than `ExpiresIn`, otherwise every check reports drift
forever. `ExpiresIn` above the instance maximum is not detectable locally and surfaces as the
platform error of FR-052.

`Days` is a distinct integer type parsed from `<N>d`, keeping a duration in days from being
confused with a `time.Duration` (Constitution IV, concrete types).

---

## `internal/gitrepo` — the local working copy

### WorkingCopy

| Field | Type | Notes |
|---|---|---|
| `Root` | `string` | From `git rev-parse --show-toplevel` (FR-001) |
| `Remote` | `string` | The name inspected, default `origin` |
| `RemoteURL` | `string` | Raw, as git reports it |

### RemoteRef

Parsed from `RemoteURL` (FR-002). Accepts SSH (`git@host:owner/repo.git`), SSH with explicit port
(`ssh://git@host:2222/owner/repo.git`), and HTTPS (`https://host/owner/repo.git`), stripping a
trailing `.git`.

| Field | Type |
|---|---|
| `Host` | `string` |
| `Owner` | `string` |
| `Repo` | `string` |

Sentinel errors: `ErrNotARepo`, `ErrNoCommits`, `ErrNoRemote`, `ErrGitMissing` — all classified as
exit `2` (R11).

---

## `internal/forge` — observed platform state

### Forge

The interface, with exactly two implementations. Every method takes a `context.Context` first
(Constitution VI).

```text
DefaultBranch(ctx) (string, error)
SetDefaultBranch(ctx, name) error
BranchExists(ctx, name) (bool, error)
Protection(ctx, branch) (Protection, error)
SetProtection(ctx, branch, Protection) error
TagProtection(ctx) ([]string, error)
ProtectTag(ctx, pattern) error
Variable(ctx, name) (VariableState, error)
SetVariable(ctx, VariableWrite) error
```

The GitLab implementation adds the token lifecycle, which is not on the interface because it has
one platform:

```text
ProjectTokens(ctx, name) ([]ProjectToken, error)
CreateProjectToken(ctx, ProjectTokenRequest) (ProjectToken, string, error)
RevokeProjectToken(ctx, id) error
```

The second return of `CreateProjectToken` is the token value, available exactly once (R8). It is
passed directly to `SetVariable` and never stored (FR-050).

### Protection

| Field | Type | Notes |
|---|---|---|
| `Exists` | `bool` | False means the branch is unprotected |
| `AllowForcePush` | `bool` | GitHub `non_fast_forward` rule absent; GitLab `allow_force_push` |
| `AllowDelete` | `bool` | GitHub `deletion` rule absent; **always false on GitLab** (R9) |
| `PushAccessLevel` | `AccessLevel` | GitLab only; zero value on GitHub |

### VariableState

What the platform reports back.

| Field | Type | Notes |
|---|---|---|
| `Exists` | `bool` | |
| `Masked` | `bool` | GitLab only |
| `Protected` | `bool` | GitLab only |
| `Value` | `string` | GitLab only; **always empty on GitHub** — secrets are write-only |
| `ValueReadable` | `bool` | False on GitHub, so FR-027 never compares |

`ValueReadable` is an explicit field rather than an inference from the platform, so the comparison
in `internal/compliance` reads as a fact about the value rather than a special case about GitHub.

### ProjectToken

| Field | Type |
|---|---|
| `ID` | `int` |
| `Name` | `string` |
| `ExpiresAt` | `time.Time` |
| `Active` | `bool` |
| `Revoked` | `bool` |

### Ruleset (GitHub only)

| Field | Type | Notes |
|---|---|---|
| `ID` | `int64` | |
| `Name` | `string` | Always `forgectl`; a ruleset with any other name is never modified (R14, open item 2) |
| `Target` | `string` | `branch` or `tag` |
| `Include` | `[]string` | `refs/heads/main`, `refs/tags/v*` |
| `Rules` | `[]string` | `deletion`, `non_fast_forward` |

---

## `internal/compliance` — evaluation

### CheckResult

| Field | Type | Notes |
|---|---|---|
| `ID` | `string` | `branch`, `protection`, `tags`, `vars:<NAME>` (FR-021) |
| `Domain` | `Domain` | `branch`, `protection`, `vars` — tags belongs to `protection` (FR-036) |
| `Status` | `CheckStatus` | `pass`, `fail`, `skip` (FR-022) |
| `Expected` | `string` | Set on `fail` |
| `Actual` | `string` | Set on `fail` |
| `Reason` | `string` | Set on `skip` |
| `Fixable` | `bool` | False for the `trunk` case of FR-039 |
| `Generator` | `*GeneratorStatus` | Set only for generated variables |

**Invariant**: `Expected`, `Actual`, and `Reason` are message fields and MUST NOT contain a value
(FR-054). The renderer never receives one — a value reaches only `internal/values` and
`forge.SetVariable`.

### GeneratorStatus

| Field | Type |
|---|---|
| `Kind` | `string` |
| `ExpiresAt` | `time.Time` |
| `RotateInDays` | `int` |

### Report

| Field | Type |
|---|---|
| `Repository` | `string` |
| `Instance` | `Instance` |
| `Profiles` | `[]string` |
| `Checks` | `[]CheckResult` |
| `Summary` | `Summary` (`pass`, `fail`, `skip` counts) |

**Exit code derivation** (CLI-002): `fail > 0` → `3`; otherwise `0`. Skips never produce drift.

### Plan and Action

| Field | Type | Notes |
|---|---|---|
| `Actions` | `[]Action` | Ordered branch → protection → tags → vars (FR-034) |

| Action field | Type | Notes |
|---|---|---|
| `Domain` | `Domain` | For `--only` / `--skip` filtering |
| `Description` | `string` | The line shown in the plan preview; contains no value |
| `Destructive` | `bool` | Drives the confirmation of CLI-003 |

An empty `Plan` means a compliant repository: no confirmation is requested and no mutating call is
made (FR-035).

---

## `internal/values` — resolution

### Resolver

Resolves in the order of FR-044: `--var-file` → `Value`/`ValueRef` → `Generator` → concealed
prompt when a TTY is attached → error listing every missing name.

**Invariants**

- Resolution runs to completion for every selected variable **before** the first write, so a
  missing value cannot leave the repository half-converged (FR-044).
- A resolved value is held in memory only, passed to exactly one `SetVariable` call, and never
  logged, wrapped into an error, or written to disk (FR-050, FR-054).
- The generator branch is the one source that mutates the platform while resolving, which is why
  FR-051 exists: a created token whose variable write then fails cannot be recovered.

---

## State transitions

### Generated token (spec §7, R8)

```text
absent ──apply──> active(expires = today + ExpiresIn)
active(remaining > RotateBefore) ──check──> pass
active(remaining <= RotateBefore) ──check──> fail "expires in N days"
                                  ──apply──> new token active, previous revoked
several active with the same name ──check──> fail "ambiguous"
                                  ──apply──> exactly one active, all others revoked
any state ──apply --force-rotate──> new token active, previous revoked
```

**Invariant** (spec §7.1): after a successful apply, exactly one active token carries `TokenName`,
and the CI variable holds its only copy. Revocation happens strictly after the variable write
succeeds (FR-048), so an interruption leaves an extra active token rather than a variable holding a
revoked one.

### Default branch (spec §6.1)

```text
default = master, main absent  ──> rename, push -u, set default, set-head [, delete master]
default = master, main exists  ──> set default, set-head [, delete master]   (no rename, no push)
default = main                 ──> pass
default = anything else        ──> fail, Fixable = false, manual hint (FR-039)
```

---

## Type-level notes

- `Platform`, `CheckStatus`, `Domain`, and `AccessLevel` are string-backed named types with
  hand-written `String()` methods and explicit parse functions that reject unknown values (R13).
- `Days` is a named `int`, parsed from `<N>d`, never a `time.Duration`.
- No type parameter appears anywhere: `forge.Forge` has two implementations, which is polymorphism
  through an interface, not the generic Constitution IV defers until three concrete types exist.
