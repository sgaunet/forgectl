# Feature Specification: Forge Convention Check & Apply

**Feature Branch**: `001-forge-conventions`

**Created**: 2026-08-30

**Status**: Draft

**Input**: User description: "forgectl — Specification v0.3 (consolidated): a CLI that detects the forge hosting a repository's remote, checks the repository against declared conventions (default branch, branch protection, CI variables per project type), and applies the required fixes, on a check/apply model with idempotent convergence and no value ever printed."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Audit a repository against conventions (Priority: P1)

A maintainer working inside a git clone runs a single read-only command and learns, in one screen,
which of their conventions the repository violates: whether the default branch is what they
declared, whether that branch is protected, and — once profiles are involved — whether the CI
variables their pipelines need are present and correctly attributed. Nothing is modified.

**Why this priority**: The audit is the whole value proposition on its own. A maintainer with a
dozen repositories cannot remember which ones drifted; being told is useful even if they then fix
things by hand. It is also the foundation every other story reads from.

**Independent Test**: Point the tool at a repository whose default branch is `master`, run the
read-only command, and confirm it reports the branch drift, reports nothing else as changed, and
signals drift through its exit status. Verify the repository and the platform are untouched.

**Acceptance Scenarios**:

1. **Given** a clone whose remote is on a declared instance and whose default branch is `master`
   while the configured default is `main`, **When** the maintainer runs the check command,
   **Then** the report shows the `branch` check as failed with expected `main` and actual
   `master`, and the process signals drift.
2. **Given** a fully compliant repository, **When** the maintainer runs the check command,
   **Then** every check reports as passing, the drift count is zero, and the process signals
   success.
3. **Given** a clone whose target branch does not yet exist on the platform, **When** the check
   runs, **Then** the `protection` check reports as skipped with the reason stated, and the skip
   is counted separately from failures.
4. **Given** the maintainer is in a subdirectory of the clone, **When** they run any command,
   **Then** the enclosing working copy is found and used.
5. **Given** the maintainer runs the detection command, **Then** the repository owner and name,
   the instance name, the host, the platform, and the API base URL are reported.

---

### User Story 2 - Converge branch and protection settings (Priority: P2)

Having seen the drift, the maintainer runs the apply command. The tool first shows exactly what it
intends to do, asks for confirmation, then renames the default branch, pushes it, sets it as the
platform default, protects it against force-push and deletion, and protects the release tag
patterns the selected profiles declare. Running it again changes nothing.

**Why this priority**: This is the payoff of the audit and covers the two conventions that apply to
every repository regardless of project type. It is deliverable before any CI-variable work exists.

**Independent Test**: On a repository with default branch `master` and no protection, run apply,
confirm the plan, and verify the platform now reports `main` as the default and as protected. Run
apply a second time and verify the plan is empty and nothing changes.

**Acceptance Scenarios**:

1. **Given** a repository whose default branch is `master` and which has no `main`, **When** apply
   runs and is confirmed, **Then** `main` is created from `master`, pushed, set as the platform
   default, and the local remote head is updated; the old remote branch survives unless deletion
   was explicitly requested.
2. **Given** a repository where both `main` and `master` already exist, **When** apply runs,
   **Then** only the default-branch switch, the remote-head update, and the optional old-branch
   deletion are performed — no local rename is attempted.
3. **Given** a repository whose default branch is neither `master` nor `main`, **When** apply runs,
   **Then** the branch step reports as failed and not auto-fixable, a manual-fix hint is printed,
   and the remaining steps still run.
4. **Given** the default branch was switched, **When** apply completes, **Then** the maintainer is
   warned that open merge or pull requests targeting the old branch need manual retargeting, and
   is given the command other clones must run.
5. **Given** an already-compliant repository, **When** apply runs, **Then** the plan is empty, no
   confirmation is requested, and no platform call that mutates state is made.
6. **Given** the maintainer declines the confirmation prompt, **When** the plan is shown,
   **Then** nothing is modified and the command reports that it was cancelled.

---

### User Story 3 - Keep CI variables in sync from a profile (Priority: P3)

The maintainer names one or more project-type profiles — cumulative, deduplicated — and the tool
checks that every variable those profiles declare exists on the platform with the right
attributes, then writes the missing or drifted ones from values declared once in the local
configuration.

**Why this priority**: CI variables are the per-project-type half of the conventions, and the part
that is most tedious by hand. It depends on nothing from stories 1 and 2 beyond detection, but it
is worth less on its own than knowing the repository's branch state.

**Independent Test**: With a profile declaring two variables and a configuration providing their
values, run check on a repository that has neither, confirm both are reported missing, run apply,
and confirm both now exist on the platform with the declared attributes — and that no value
appeared anywhere in the output.

**Acceptance Scenarios**:

1. **Given** two profiles are named and both declare a variable of the same name with identical
   attributes, **When** either command runs, **Then** the variable is handled exactly once.
2. **Given** two profiles declare the same variable name with conflicting attributes or
   conflicting value sources, **When** the configuration loads, **Then** the command fails with a
   configuration error naming the variable and both profiles, before any platform call.
3. **Given** no profile is named on the command line and the repository root holds a per-repository
   file listing profiles, **When** the command runs, **Then** those profiles are used.
4. **Given** no profile is named anywhere, **When** the command runs, **Then** only the branch and
   protection checks run and a warning states that CI variables were not checked.
5. **Given** a variable whose value is declared in the shared value store, **When** apply runs against GitLab,
   **Then** a differing value is reported as drift and overwritten; against GitHub, whose secrets
   are write-only, the configured value is written on every run and never reported as drift.
6. **Given** a required value is available from no source and no interactive terminal is attached,
   **When** apply runs, **Then** it fails listing every missing value by name, before making any
   change.
7. **Given** a multiline value that the platform refuses to mask, **When** apply runs, **Then** the
   write is retried unmasked and a warning explains why.

---

### User Story 4 - Self-rotating release token (Priority: P4)

A Go project's release pipeline needs a platform token that is never a human's personal token,
always carries an expiry, and is replaced before it expires. The maintainer declares the variable
as generated; the tool creates the project-scoped token, writes it straight into the CI variable,
revokes the one it replaces, and from then on reports how many days remain.

**Why this priority**: It removes the single most error-prone manual step in release automation,
but it only matters once variables (story 3) work, and only on GitLab, which alone offers project
access tokens.

**Independent Test**: Declare a generated variable on a project with no such token, run check and
confirm it reports the token missing, run apply, then confirm exactly one active token with the
configured name exists, the CI variable is set, and a second check passes reporting the remaining
lifetime.

**Acceptance Scenarios**:

1. **Given** no active token with the configured name exists, **When** check runs, **Then** the
   variable's check fails with "token missing".
2. **Given** more than one active token carries the configured name, **When** check runs, **Then**
   the check fails as ambiguous and recommends rotation.
3. **Given** an active token whose remaining lifetime is at or below the configured rotation
   threshold, **When** check runs, **Then** the check fails and states the number of days
   remaining.
4. **Given** a drifted or expiring token, **When** apply runs, **Then** a new token is created with
   the configured name, scopes, role, and expiry, the CI variable is written from it immediately,
   and every other token of that name is revoked when revocation is enabled — leaving exactly one
   active token.
5. **Given** the token was created but writing the CI variable then failed, **When** apply reports,
   **Then** it states that the token remains active and that apply must be rerun.
6. **Given** the requested lifetime exceeds the instance maximum, **When** apply runs, **Then** it
   fails with a message stating the maximum permitted.
7. **Given** the repository is on a GitHub instance, which has no project access token equivalent,
   **When** either command runs, **Then** the generated variable is skipped with a warning and the
   run does not fail.
8. **Given** the operating token lacks the rights to list or create project tokens, **When** check
   runs, **Then** the variable is skipped with that reason and check does not treat it as a
   failure.
9. **Given** a healthy, non-expiring token, **When** apply runs with the forced-rotation flag,
   **Then** it is rotated anyway.

---

### User Story 5 - Discover what a profile does (Priority: P5)

Before naming a profile, the maintainer lists the available ones and inspects a single profile to
see which variables it manages, how each is attributed, and where its value comes from — without
any value being revealed.

**Why this priority**: Pure discoverability. Everything works without it; it makes the rest usable
by someone who did not write the configuration.

**Independent Test**: With no configuration file at all, list the profiles and confirm the three
built-in ones appear; add a configuration overriding one of them and confirm the listing reflects
the override rather than duplicating the name.

**Acceptance Scenarios**:

1. **Given** no configuration file exists, **When** the profiles are listed, **Then** the three
   built-in profiles appear.
2. **Given** the configuration declares a profile whose name matches a built-in one, **When** the
   profiles are listed, **Then** the name appears once and reflects the configured definition.
3. **Given** a profile is shown in detail, **Then** each variable's name, attributes, value-source
   kind, and the profile's protected tag patterns are displayed, and no value is displayed.

---

### Edge Cases

- The working directory is not inside a git working copy → the command fails with a distinct,
  explicit error before touching configuration or network.
- The repository has no commits → an explicit error naming that condition.
- HEAD is detached → operations proceed normally, because every operation targets named refs.
- The remote host matches no configured instance and no built-in one → error naming the host and
  the configuration file path where it should be declared.
- The named remote does not exist → error naming the remote and listing those that do.
- The credential environment variable named by the instance is unset or empty → error naming the
  variable, raised before any network call.
- The default branch is neither the old nor the new conventional name → reported as not
  auto-fixable, with a manual-fix hint; other domains still run.
- Both the old and the new branch already exist → the local rename is skipped, the remaining steps
  run.
- Apply fails partway → the report states which actions succeeded and which did not; rerunning
  apply converges from wherever it stopped.
- The platform rate-limits the tool → requests are retried a bounded number of times with backoff
  before the operation is reported as failed.
- The same variable name is declared by two profiles with conflicting attributes → configuration
  error, raised at load time.
- A generated variable is encountered on a GitHub instance → skipped with a warning, not a failure.
- The operating credential cannot list or create project tokens → the generated variable's check is
  skipped with that reason and does not count as drift.
- A generated token is created but the subsequent variable write fails → the token stays active and
  the report says to rerun apply.
- The requested token lifetime exceeds the instance maximum → explicit error stating the maximum.
- The platform refuses masking for a multiline value → the write is retried unmasked, with a
  warning.
- The configuration file or an override file is readable by anyone but the owner → the tool refuses
  to start unless the bypass flag is given, and names the command that fixes the permissions.
- A pipeline is in flight when the token it already resolved is revoked → documented consequence of
  the default revocation behaviour; disabling revocation defers the cleanup to the next rotation.
- The user interrupts the tool mid-apply → the run stops at the current step, reports what
  completed, and leaves a state that rerunning apply converges.

## Requirements *(mandatory)*

### Functional Requirements

**Forge and repository detection**

- **FR-001**: System MUST locate the enclosing git working copy from any subdirectory, and MUST
  fail with a distinct error when the working directory is not inside one.
- **FR-002**: System MUST read the URL of the named remote (default `origin`) and derive the host,
  the owner, and the repository name from it, accepting SSH and HTTPS forms with an optional
  explicit port, and stripping a trailing `.git`.
- **FR-003**: System MUST resolve the derived host against the configured instances; when no
  configured instance matches, it MUST fall back to built-in definitions for `github.com` and `gitlab.com`;
  when neither matches, it MUST fail with a message naming the unknown host and the configuration
  file path in which to declare it.
- **FR-004**: The detection command MUST report the repository owner and name, the instance name,
  the host, the platform kind, and the API base URL.
- **FR-005**: An instance's credential MUST be read from the environment variable that the instance
  names; a missing or empty variable MUST fail before any network call, naming the variable.

**Configuration**

- **FR-006**: System MUST load its configuration from the path given on the command line, or from a
  fixed default location under the user's configuration directory.
- **FR-007**: System MUST refuse to start when the configuration file or an override file is
  readable or writable beyond its owner, unless the bypass flag is given; the refusal MUST name the
  file, its current mode, and the command that corrects it.
- **FR-008**: System MUST ship three built-in profiles — `ansible-role`, `go-release`, and
  `ssh-deploy` — usable with no configuration file present. A configured
  profile of the same name MUST replace the built-in one; a new name MUST extend the set.
- **FR-009**: A variable definition MUST carry exactly one value source — an inline literal, a
  reference into the shared value store, or a generator. Zero or more than one MUST be a
  configuration error naming the variable.
- **FR-010**: A reference MUST resolve against the shared value store; an unresolved reference MUST
  be a configuration error naming both the variable and the missing key.
- **FR-011**: Variable attributes MUST be: secret (default true), masked (default false), protected
  (default false).
- **FR-012**: Generator attributes MUST default to: token name `forgectl`, scopes `[api]`, role
  `maintainer`, lifetime `180d`, rotation threshold `60d`, revoke-replaced `true`.
- **FR-013**: Durations MUST be expressed as a whole number of days in the form `<N>d`; any other
  form MUST be a configuration error.
- **FR-014**: A profile MAY declare protected tag patterns; the default MUST be none.
- **FR-015**: The configured default branch MUST default to `main`, and branch protection MUST
  default to enabled, force-push denied, deletion denied, and direct push restricted to
  maintainers.
- **FR-016**: All configuration MUST be validated before any platform call, and every validation
  failure MUST name the offending element.

**Profile selection**

- **FR-017**: Profiles MUST be taken from the positional arguments; when none are given, from the
  per-repository file at the repository root; when neither supplies any, no profile is selected.
- **FR-018**: Multiple profiles MUST be combined as a union of their variables, deduplicated by
  name. Two profiles declaring the same variable name with differing attributes or differing value
  sources MUST be a configuration error naming the variable and the profiles.
- **FR-019**: With no profile selected, only the branch and protection checks MUST run, and a
  warning MUST state that CI variables were not checked.
- **FR-020**: The profile listing MUST show every available profile, built-in and configured; the
  profile detail view MUST show each variable's name, attributes, value-source kind, and the
  profile's protected tag patterns, and MUST NOT show any value.

**Check catalog**

- **FR-021**: The catalog MUST comprise: the default-branch check, the branch-protection check, the
  protected-tags check, and one check per selected variable.
- **FR-022**: Every check MUST report exactly one of pass, fail, or skip; a fail MUST carry the
  expected and actual state; a skip MUST carry its reason.
- **FR-023**: The default-branch check MUST compare the platform's default branch against the
  configured one.
- **FR-024**: The protection check MUST verify that force-push is denied, deletion is denied, and
  — on GitLab, which alone models it — that direct push is restricted to the configured access
  level on the target branch. It MUST skip with a stated reason when that branch does not exist.
- **FR-025**: The protected-tags check MUST verify that each tag pattern declared by the selected
  profiles is protected.
- **FR-026**: A variable check MUST verify presence and the attributes the platform supports. The
  mapping MUST be: on GitHub, a secret variable is an Actions secret and a non-secret variable is
  an Actions variable, while the masked and protected attributes have no equivalent; on GitLab,
  every variable is a CI variable carrying the masked and protected attributes directly.
  Attributes the platform has no equivalent for MUST NOT be reported as drift.
- **FR-027**: GitLab CI variable values are readable, so a value differing from the configured one
  MUST be reported as drift. GitHub Actions secrets are write-only, so their values MUST NOT be
  compared and MUST NOT be reported as drift.
- **FR-028**: A generated variable's check MUST report: missing token; more than one token of that
  name as ambiguous; remaining lifetime at or below the rotation threshold as failing, stating the
  days remaining; a missing CI variable as failing; and otherwise pass.
- **FR-029**: A generated variable on a GitHub instance, which has no project access token
  equivalent, MUST be skipped with a warning and MUST NOT fail the run.
- **FR-030**: Insufficient rights to list or create project tokens MUST cause the affected check to
  skip with that reason, and MUST NOT count as drift.
- **FR-031**: The check command MUST NOT modify any local or platform state.
- **FR-032**: Every variable declared by the selected profiles MUST target the single instance
  hosting the detected repository. A variable definition MUST NOT carry a platform or an instance
  field, and one run MUST NOT write to more than one instance. A profile whose variables suit
  only one platform is the maintainer's concern, not the tool's: the platform mapping in FR-026
  already resolves every attribute on whichever instance the repository lives on.

**Apply**

- **FR-033**: Apply MUST print the actions it intends to take before performing any of them, and
  MUST require confirmation unless the confirmation-skipping flag is given.
- **FR-034**: Apply MUST perform its work in a fixed order: default branch, then protection
  including tags, then variables.
- **FR-035**: Apply MUST be idempotent: on an already-compliant repository the plan MUST be empty
  and no state-changing platform call MUST be made.
- **FR-036**: Apply MUST accept a scope restriction naming the domains branch, protection, and
  variables, as a comma-separated list, in either an inclusive or an exclusive form; the tags work
  MUST belong to the protection domain. Supplying both forms at once MUST be a usage error.
- **FR-037**: When the platform default is the old conventional branch name and the new one does
  not exist, apply MUST rename the local branch (creating it from the remote branch when it is
  absent locally), push it with upstream tracking, set it as the platform default, and update the
  local remote head.
- **FR-038**: When both branch names already exist, apply MUST skip the rename and push, and
  perform only the default-branch change and the remote-head update.
- **FR-039**: When the platform default is neither conventional name, apply MUST report the branch
  domain as failed and not auto-fixable, print a manual-fix hint, and continue with the remaining
  domains.
- **FR-040**: After a default-branch switch, apply MUST warn that open merge or pull requests
  targeting the old branch require manual retargeting, and MUST print the command other clones need
  to run.
- **FR-041**: The old remote branch MUST be deleted only when explicitly requested, and only after
  the new default branch is in place.
- **FR-042**: Apply MUST create a missing variable and update a drifted one. On GitHub, where
  values cannot be read back, it MUST write the configured value on every run, which converges to
  the same result.
- **FR-043**: When the platform refuses to mask a multiline value, apply MUST retry the write
  unmasked and warn.
- **FR-044**: Values MUST be resolved in this order: the one-off override file; the inline literal
  or shared-store reference in the configuration; the generator; an interactive prompt with
  concealed input when a terminal is attached. If none supplies a required value, apply MUST fail
  listing every missing value, before making any change.
- **FR-045**: On partial failure, apply MUST report which actions succeeded and which did not, and
  rerunning apply MUST converge from that state.
- **FR-046**: Platform rate limiting MUST be handled by a bounded number of retries with backoff
  before the operation is reported as failed.

**Generated tokens**

- **FR-047**: Apply MUST create a GitLab project access token with the configured name, scopes,
  role, and
  an expiry computed from the configured lifetime, and MUST write its value into the CI variable
  immediately, in the same run.
- **FR-048**: When revocation of replaced tokens is enabled, apply MUST revoke every other token
  carrying the configured name, only after the CI variable has been written successfully.
- **FR-049**: At most one active token with the configured name MUST exist per project after a
  successful apply.
- **FR-050**: A generated token MUST NOT be written to any local file, cache, or state by the tool;
  the CI variable MUST be its only persisted copy.
- **FR-051**: When the token was created but writing the CI variable failed, apply MUST report that
  the token remains active and that apply must be rerun.
- **FR-052**: When the requested lifetime exceeds the instance maximum, apply MUST fail with a
  message stating the maximum permitted.
- **FR-053**: The forced-rotation flag MUST rotate generated tokens even when no drift was
  detected.

**Confidentiality of values**

- **FR-054**: No value — configured, referenced, generated, or prompted — MUST ever appear in
  standard output, in the machine-readable output, in a log line, in a progress message, or in an
  error message. Only statuses such as missing, differs, or the remaining lifetime in days MUST be
  reported.
- **FR-055**: The machine-readable output MUST use one schema shared by the check and apply
  commands, and generated-variable entries MUST carry their generator name, expiry date, and
  remaining days.
- **FR-056**: When a file holding values — an override file or the per-repository file — lies
  inside the working copy and git does not ignore it, the tool MUST refuse to run, naming the
  file and the ignore entry that would resolve it. The same file, ignored by git, MUST be
  accepted without a warning. This refusal has no bypass: unlike the permission check of FR-007,
  `--allow-insecure-config` MUST NOT override it, because the file is one `git add` away from
  being published.
- **FR-057**: The refusal in FR-056 MUST be raised at load time, before any platform call and
  before any value is read from the file.

### CLI Contract Requirements *(mandatory)*

- **CLI-001**: Stdout MUST carry the command's data only — the detection facts, the compliance
  report, the applied-action report, or the profile listing — machine-parseable and selectable with
  `--output=text|json`. Stderr MUST carry logs, warnings, progress, errors, and the interactive
  confirmation prompt.
- **CLI-002**: Exit codes MUST be documented in `--help` and MUST be: `0` the command succeeded
  and the repository is compliant; `1` runtime failure — something broke after work began
  (network, authentication rejected, a platform error, a git command failing, an apply that
  completed only in part); `2` usage error — the invocation or the configuration was wrong and
  nothing was attempted (an unknown flag or an unusable combination of flags, an unknown profile
  name, invalid configuration, a configuration file whose permissions are too wide, a
  values-bearing file that git does not ignore, a working directory outside a git working copy,
  an unknown remote, a host matching no instance, an unset credential variable); `3` the command
  succeeded and drift remains — reported by check whenever any check fails, and by apply when it
  finishes leaving drift it declared not auto-fixable. Where more than one applies, the lowest
  code that is not `0` wins: a runtime failure during a drifted check exits `1`, not `3`.
- **CLI-003**: Destructive actions added by this feature, each requiring `--yes` or an interactive
  confirmation: renaming and pushing the default branch, changing the platform default branch,
  deleting the old remote branch (`--delete-old-branch`), changing branch and tag protection,
  overwriting an existing CI variable, and creating and revoking project-scoped tokens (including
  `--force-rotate`). The check, detect, and profile commands are read-only and require no
  confirmation.
- **CLI-004**: Configuration this feature reads, in precedence order flags > environment > config
  file > defaults — configuration file path (`--config`, `FORGECTL_CONFIG`, default
  `~/.config/forgectl/config.yaml`); remote name (`--remote`, `FORGECTL_REMOTE`, default `origin`);
  output format (`--output`, `FORGECTL_OUTPUT`, default `text`); colour suppression
  (`--no-color`, `NO_COLOR`, default: colour only when stdout is a terminal); verbosity
  (`--verbose`, `--quiet`, `FORGECTL_LOG_LEVEL`). Instance credentials come only from the
  environment variable each instance names, and are never read from flags or the configuration
  file.
- **CLI-005**: Every platform call is a long-running operation and MUST cancel cleanly on `SIGINT`
  and `SIGTERM`, MUST carry a per-request timeout (default 30 seconds), and MUST bound its retries
  (at most 3, with exponential backoff). Interrupting apply MUST leave a state that rerunning apply
  converges; an interruption between creating a token and writing its variable MUST be reported as
  such.

### Key Entities

- **Instance**: A declared forge endpoint — a name, a host, a platform kind (`github` or `gitlab`),
  an API base URL, and
  the name of the environment variable holding its credential. Two built-in instances cover the
  public hosts.
- **Repository**: The detected target — owner, name, and the instance that hosts it — derived from
  a remote of the local working copy.
- **Profile**: A named project type — a set of variable definitions plus optional protected tag
  patterns. Three exist built into the tool; configuration may replace or extend them.
- **Variable definition**: A name, exactly one value source (literal, store reference, or
  generator), and the attributes secret, masked, and protected.
- **Value store**: A set of named values declared once in the configuration and referenced by any
  number of profiles, so a value shared by several project types is written down a single time.
- **Generator**: The description of a GitLab project access token the tool creates and rotates on
  the maintainer's behalf — token name, scopes, role, lifetime, rotation threshold, and whether
  replaced tokens are revoked.
- **Check result**: One catalog entry's outcome — its identifier, its status, and either the
  expected and actual state or the reason it was skipped; for generated variables, also the
  expiry and remaining days.
- **Plan**: The ordered list of actions apply intends to perform, shown before anything is changed
  and empty when the repository is already compliant.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A maintainer with no configuration file at all can audit a repository hosted on
  `github.com` or `gitlab.com` with a single command, having set only the platform credential in
  their environment.
- **SC-002**: Running apply twice in succession on the same repository produces an empty plan and
  zero state-changing platform calls on the second run.
- **SC-003**: Across every output path — human-readable, machine-readable, logs, and error
  messages — the number of configured or generated values disclosed is zero, verified by tests that
  assert known values never appear.
- **SC-004**: A repository failing every check reaches full compliance in a single apply run,
  requiring no step in the platform's web interface, except for conditions the tool explicitly
  declares not auto-fixable.
- **SC-005**: A generated release token is replaced before it expires with no human action, given
  the tool runs at least once during the rotation window, which is at least 60 days wide.
- **SC-006**: An automated caller can distinguish "compliant", "drift found", and "failed" from the
  process outcome alone, without parsing human-readable text.
- **SC-007**: An apply interrupted at any point leaves a repository that a subsequent apply brings
  to full compliance.
- **SC-008**: Auditing a repository against a profile of up to ten variables completes in under
  ten seconds against a responsive platform.
- **SC-009**: A maintainer who did not write the configuration can discover which variables a
  profile manages, and where each value comes from, without reading the configuration file and
  without any value being revealed to them.

## Assumptions

### Resolved conflicts

Three questions were put to the author because the source specification and the project
constitution disagreed, or the source specification disagreed with itself. All three are
settled and written into the requirements above:

- **Exit codes** — the constitution's `0`/`1`/`2` semantics stand unchanged, and drift takes a
  fourth code, `3` (CLI-002). No constitution amendment is needed.
- **Variable scoping** — a run touches exactly one instance, the one hosting the detected
  repository; variables carry no platform or instance field (FR-032). The cross-instance rows in
  the source specification's sample output are dropped as an inconsistency with its own schema.
- **Values inside the working copy** — a values-bearing file that git does not ignore is a
  refusal, not a reminder; the same file, ignored, is accepted (FR-056). A value that cannot be
  committed is not in the repository, so the per-repository override workflow survives intact.

### Defaults adopted

- **Constitution reconciliation — output flag**: The source specification names a `--json` flag;
  the project constitution requires `--output=text|json`. This specification adopts `--output` as
  the contract and treats `--json` as a documented alias for `--output=json`, satisfying both.
- **Constitution reconciliation — quiet mode**: The source specification lists only `--verbose`;
  the constitution requires both `--quiet` and `--verbose`. Both are assumed present, controlling
  the level of the stderr log stream only, never the stdout data.
- **Constitution reconciliation — colour**: `NO_COLOR` is honoured, and colour, spinners, and
  progress indicators are suppressed whenever stdout is not a terminal, in addition to the explicit
  `--no-color` flag.
- **Environment overrides**: The source specification defines no environment variables for the
  global options; the constitution requires a stated flags > environment > config file > defaults
  precedence, so `FORGECTL_`-prefixed equivalents are assumed for the global options, as listed in
  CLI-004.
- **Timeouts and retries** — *confirmed at planning*: a 30-second per-request timeout and at most
  three retries with exponential backoff, enforced by one shared transport that both platform
  clients are given, so the retry policy is forgectl's and not the client library's.
- **Platform attribute asymmetry**: Where a platform has no equivalent for an attribute — masking
  and protection on GitHub, whose secrets are always encrypted, or a push access level GitHub does
  not model — that attribute is ignored rather than reported as drift.
- **Check outcome with skips only**: A run whose checks are all pass or skip, with no failure, is
  treated as compliant.
- **Credential source**: Instance credentials are read only from the environment. The shared value
  store, by contrast, holds values in the local configuration file, whose permissions are enforced;
  this file is outside any repository, so no value is stored in a repository by design.
- **Protected tags on GitHub** — *resolved during planning*: GitHub's tag protection API was sunset
  and has returned NULL data since 2024-08-30, so calling it appears to succeed while protecting
  nothing. Protected tags, and branch protection with them, are delivered through repository
  rulesets instead. No check degrades to a skip. See `research.md` R2.
- **Dependency approval** — *resolved during planning*: six direct modules were proposed to and
  approved by the author on 2026-08-30 (see `plan.md`). One of them, `gopkg.in/yaml.v3`, is
  archived and unmaintained; it was chosen over the maintained alternative and is recorded as a
  deliberate exception in the plan's Complexity Tracking.
- **Scope**: Repository creation, pipeline file management, group- or organisation-level policy,
  multi-repository batches, project-type auto-detection, additional forge platforms, and value
  resolution from an external secret manager are all outside this feature.
