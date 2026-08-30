---
description: "Task list for Forge Convention Check & Apply"
---

# Tasks: Forge Convention Check & Apply

**Input**: Design documents from `/specs/001-forge-conventions/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md),
[data-model.md](./data-model.md), [contracts/](./contracts/), [quickstart.md](./quickstart.md)

**Tests**: Test tasks are MANDATORY. Constitution VII (Test-First, Black Box) is non-negotiable:
every behaviour change arrives with the test that fails without it, so each phase below opens with
its failing tests. Tests live in `package <pkg>_test`; internals are bridged through
`export_test.go` inside the package, never by moving a test back into it.

**Organization**: Tasks are grouped by user story so each story can be implemented, tested, and
delivered independently.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (US1–US5)
- Every task names the exact file path it touches

## Path Conventions

- **Command wrapper**: `cmd/forgectl/` — parse, validate, call, format only (Constitution III)
- **Business logic**: `internal/<domain>/` — MUST import no CLI package
- **Tests**: `internal/<domain>/<file>_test.go` in `package <domain>_test`
- **Golden files**: `internal/<domain>/testdata/`
- `utils`, `helpers`, `common`, and `base` packages are forbidden — every package is named for its
  domain

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Module, dependencies, and package skeleton. The repository already carries
`mise.toml`, `Taskfile.yml`, `.golangci.yml`, `.goreleaser.yaml`, and `.github/workflows/`; those
tasks verify and align rather than create.

- [X] T001 Initialize `go.mod` at the repository root with module path `github.com/sgaunet/forgectl`, a `go 1.26.1` directive, and a `toolchain` directive matching `mise.toml` (R1, Constitution II)
- [X] T002 Add the six author-approved direct dependencies to `go.mod`: `github.com/spf13/cobra`, `gopkg.in/yaml.v3`, `golang.org/x/crypto`, `golang.org/x/term`, `github.com/google/go-github/vNN` (confirm the current major at `go get` time — research.md open item 1), `gitlab.com/gitlab-org/api/client-go`; add no test dependency
- [X] T003 [P] Create the package skeleton from plan.md: `cmd/forgectl/main.go` plus a `doc.go` in each of `internal/gitrepo/`, `internal/config/`, `internal/forge/`, `internal/forge/github/`, `internal/forge/gitlab/`, `internal/forge/forgetest/`, `internal/values/`, `internal/compliance/`, `internal/apply/`, `internal/report/`, so `go build ./...` succeeds on an empty tree
- [X] T004 [P] Align `Taskfile.yml` with the four gates of the constitution's Development Workflow: `lint`, `test` (`go test -count=2 -race ./...`), `build` (`CGO_ENABLED=0`, `-trimpath`), `release`
- [X] T005 [P] Confirm `.goreleaser.yaml` builds with `CGO_ENABLED=0` and `-trimpath` and publishes SHA-256 checksums and an SBOM (Constitution II)
- [X] T006 [P] Confirm `.golangci.yml` targets golangci-lint v2 and that `.github/workflows/test.yml` and `.github/workflows/linter.yml` run the lint, race test, and `govulncheck ./...` gates

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The CLI shell, the local git layer, the configuration layer, the forge abstraction, and
the shared result document. Every user story reads from these.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

### CLI shell — `cmd/forgectl`

- [X] T007 [P] Test the exit-code classifier in `cmd/forgectl/main_test.go` (`package main_test`) covering `0` compliant, `1` runtime failure, `2` usage error, `3` drift, and the "lowest non-zero wins" rule of CLI-002
- [X] T008 Implement the exit-code classifier in `cmd/forgectl/main.go` mapping the R11 sentinel set (`config.ErrInvalid`, `config.ErrPermissions`, `config.ErrValuesInRepo`, `gitrepo.ErrNotARepo`, `gitrepo.ErrNoCommits`, `gitrepo.ErrNoRemote`, `gitrepo.ErrGitMissing`, `forge.ErrUnknownHost`, `forge.ErrNoCredential`, cobra flag errors) to `2`, everything else to `1`, and expose it through `cmd/forgectl/export_test.go`
- [X] T009 Build the cobra root command in `cmd/forgectl/main.go` with the global flags of contracts/cli.md (`--config`, `--remote`, `--output`, `--json` alias, `--no-color`, `--allow-insecure-config`, `-v/--verbose`, `--quiet`), rejecting `--verbose` with `--quiet` as a usage error (exit `2`)
- [X] T010 Configure `log/slog` writing to stderr at the level `--verbose`/`--quiet`/`FORGECTL_LOG_LEVEL` select, and suppress colour, spinners, and progress whenever `NO_COLOR` is set or stdout is not a TTY, in `cmd/forgectl/main.go` (CLI-004, Constitution V)
- [X] T011 Create the root `context.Context` cancelled on `SIGINT` and `SIGTERM` in `cmd/forgectl/main.go`, passed to every domain call (CLI-005, Constitution VI)

### Local working copy — `internal/gitrepo`

- [X] T012 [P] Test remote URL parsing in `internal/gitrepo/remoteurl_test.go`: SSH `git@host:owner/repo.git`, SSH with explicit port `ssh://git@host:2222/owner/repo.git`, HTTPS `https://host/owner/repo.git`, and trailing `.git` stripping (FR-002)
- [X] T013 Implement `RemoteRef` parsing in `internal/gitrepo/remoteurl.go` returning host, owner, and repo
- [X] T014 [P] Test working-copy discovery in `internal/gitrepo/gitrepo_test.go` against real temporary repositories built with `git init` in `t.TempDir()`: discovery from a subdirectory, detached HEAD, no commits, unknown remote, `git` absent from `PATH` (FR-001, spec edge cases)
- [X] T015 Implement `WorkingCopy` discovery in `internal/gitrepo/gitrepo.go` over `git rev-parse --show-toplevel`, `git rev-parse --verify HEAD`, and `git remote get-url`, with the sentinel errors `ErrNotARepo`, `ErrNoCommits`, `ErrNoRemote`, `ErrGitMissing` (R6, R11)
- [X] T016 [P] Test git-ignore detection in `internal/gitrepo/ignore_test.go`: a file inside the working copy that git ignores, one it does not, and one outside the working copy entirely
- [X] T017 Implement the `git check-ignore -q` wrapper in `internal/gitrepo/ignore.go`, backing the refusal of FR-056

### Configuration — `internal/config`

- [X] T018 [P] Test the four-layer precedence merge in `internal/config/config_test.go`: defaults → config file → environment → flags, reading cobra's `Changed` to distinguish "unset" from "set to the zero value" (R14, CLI-004)
- [X] T019 Implement the `Config`, `Instance`, `Settings`, `BranchProtection`, `Profile`, `VariableDefinition`, `Generator`, `AccessLevel`, and `Days` types and the four-layer merge in `internal/config/config.go`, per data-model.md and `contracts/config.schema.json`
- [X] T020 [P] Test validation in `internal/config/validate_test.go`: exactly one value source per variable (FR-009), unresolved `value_ref` (FR-010), attribute defaults (FR-011), generator defaults (FR-012), `^[0-9]+d$` durations (FR-013), `rotate_before` below `expires_in`, unique instance names and hosts, and the absence of any platform or instance field on a variable (FR-032)
- [X] T021 Implement `internal/config/validate.go` so every rule runs before any platform call and every failure names the offending element and wraps `ErrInvalid` (FR-016)
- [X] T022 [P] Test the file gates in `internal/config/permissions_test.go`: a config file wider than `0600` refused with its mode and the `chmod` to run (FR-007), the same file accepted under `--allow-insecure-config`, a values-bearing file inside the working copy that git does not ignore refused with no bypass (FR-056), the same file accepted once ignored, and the refusal raised before any value is read (FR-057)
- [X] T023 Implement `internal/config/permissions.go` with `ErrPermissions` and `ErrValuesInRepo`, calling the ignore check of `internal/gitrepo`
- [X] T024 [P] Author the three built-in profiles in `internal/config/builtin_profiles.yaml`: `ansible-role` (`GALAXY_API_TOKEN`), `go-release` (a `gitlab-pat` generated variable plus its `protected_tags`), and `ssh-deploy` (multiline `SSH_PRIVATE_KEY`) (FR-008)
- [X] T025 Implement `internal/config/builtin.go` embedding that file with `//go:embed` (Constitution III), where a configured profile of the same name replaces its built-in namesake entirely with no field-level merge
- [X] T026 [P] Test profile selection and union in `internal/config/profiles_test.go`: positional arguments first, then `.forgectl.yaml` at the repository root, then none (FR-017); union deduplicated by variable name; two profiles declaring one name with differing attributes or value sources rejected naming the variable and both profiles (FR-018)
- [X] T027 Implement `.forgectl.yaml` loading, profile selection, and the deduplicating union in `internal/config/profiles.go`
- [X] T028 [P] Test built-in instance fallback and credential resolution in `internal/config/instance_test.go`: a configured host wins, `github.com` and `gitlab.com` fall back to the built-ins, an unmatched host errors naming the host and the config path (FR-003), an unset or empty `token_env` errors naming the variable before any network call (FR-005)

### Forge abstraction — `internal/forge`

- [X] T029 [P] Define the `Forge` interface and its domain types (`Protection`, `VariableState`, `ProjectToken`, `Ruleset`, `VariableWrite`, `Platform`) in `internal/forge/forge.go`, every method taking `context.Context` first (data-model.md, Constitution VI)
- [X] T030 [P] Define `ErrUnknownHost`, `ErrNoCredential`, and `ErrInsufficientRights` in `internal/forge/errors.go`
- [X] T031 [P] Test the shared transport in `internal/forge/transport_test.go` against `httptest`: a 30-second per-request timeout, retry on `429` and `5xx` bounded at 3 attempts with exponential backoff, no retry on a `4xx` other than `429`, and cancellation propagating from the context (CLI-005, FR-046)
- [X] T032 Implement the `http.RoundTripper` carrying the timeout, bounded backed-off retry, and context propagation in `internal/forge/transport.go`, injected into both official clients so the retry policy is forgectl's and not the library's (R5)
- [X] T033 Implement host-to-instance resolution and credential lookup in `internal/forge/forge.go`, returning `ErrUnknownHost` and `ErrNoCredential`
- [X] T034 [P] Write the hand-rolled fake `forge.Forge` in `internal/forge/forgetest/fake.go` — scriptable per-method responses and a call recorder, so `internal/compliance` and `internal/apply` are tested with no network and no mocking framework (R12)

### Shared result document — `internal/compliance` types and `internal/report`

- [X] T035 [P] Define `CheckResult`, `GeneratorStatus`, `CheckStatus`, `Domain`, `Summary`, `Report`, `Plan`, and `Action` in `internal/compliance/compliance.go`, with hand-written `String()` methods and parse functions rejecting unknown values (R13, data-model.md)
- [X] T036 [P] Test JSON rendering in `internal/report/json_test.go` against `specs/001-forge-conventions/contracts/output.schema.json`: required properties, the `vars:<NAME>` id pattern, `additionalProperties: false`, and one document per run
- [X] T037 Implement the shared output document and its JSON renderer in `internal/report/report.go` and `internal/report/json.go`, writing to stdout only (CLI-001)
- [X] T038 [P] Test the text renderer against golden files in `internal/report/text_test.go` and `internal/report/testdata/`, asserting no ANSI sequence when colour is disabled
- [X] T039 Implement the text renderer in `internal/report/text.go`
- [X] T040 Implement the exit-code derivation `fail > 0` gives `3`, otherwise `0`, on `compliance.Report` in `internal/compliance/compliance.go`, so a run whose checks are all pass or skip counts as compliant (CLI-002)

**Checkpoint**: The binary parses flags, finds the repository, loads and validates configuration,
resolves the instance, and can render an empty report on both streams. User story work can begin.

---

## Phase 3: User Story 1 — Audit a repository against conventions (Priority: P1) 🎯 MVP

**Goal**: A read-only `check` that reports default-branch and protection drift, and a `detect` that
reports what forge and repository were found. Nothing is modified.

**Independent Test**: Point the tool at a repository whose default branch is `master`, run
`forgectl check`, confirm it reports the branch drift, reports nothing else as changed, and exits
`3`. Verify the repository and the platform are untouched.

### Tests for User Story 1 (MANDATORY — write first, watch them fail) ⚠️

- [X] T041 [P] [US1] Test the default-branch check in `internal/compliance/branch_test.go` against the fake forge: platform default `master` versus configured `main` fails carrying expected and actual; `main` passes (FR-023, FR-022)
- [X] T042 [P] [US1] Test the branch-protection check in `internal/compliance/protection_test.go`: force-push denied, deletion denied, GitLab `push_access_level` compared and GitHub's ignored, `allow_delete` never drift on GitLab (R9), and a skip with reason when the target branch does not exist (FR-024, FR-026)
- [X] T043 [P] [US1] Test the GitHub read path in `internal/forge/github/github_test.go` with `httptest`: `GET /repos/{o}/{r}` default branch, `GET /repos/{o}/{r}/branches/{b}` existence, and protection read through `GET /repos/{o}/{r}/rulesets` matching only the ruleset named `forgectl` while still passing when another ruleset grants the protection (R2)
- [X] T044 [P] [US1] Test the GitLab read path in `internal/forge/gitlab/gitlab_test.go` with `httptest`: `GET /projects/{id}` default branch with the id URL-encoded as `owner/repo`, `GET /projects/{id}/repository/branches/{b}`, and `GET /projects/{id}/protected_branches/{name}`
- [X] T045 [P] [US1] End-to-end test in `cmd/forgectl/main_test.go` invoking the built binary against a temporary repository and an `httptest` forge: `detect` reports owner, name, instance, host, platform, and API base URL (FR-004); `check` exits `3` when drifted and `0` when compliant; stdout parses as JSON under `--output=json` while warnings and logs land on stderr (CLI-001, quickstart.md Scenarios 1–3)
- [X] T046 [P] [US1] Test that discovery works from a subdirectory of the clone end to end in `cmd/forgectl/main_test.go` (US1 acceptance scenario 4)

### Implementation for User Story 1

- [X] T047 [P] [US1] Implement `DefaultBranch`, `BranchExists`, and `Protection` on the GitHub client in `internal/forge/github/github.go`, reading protection from rulesets and never from the sunset tag-protection API (R2)
- [X] T048 [P] [US1] Implement `DefaultBranch`, `BranchExists`, and `Protection` on the GitLab client in `internal/forge/gitlab/gitlab.go`, mapping `push_access_level` `0`/`30`/`40` and reporting `AllowDelete` as always false (R9)
- [X] T049 [US1] Implement the default-branch check in `internal/compliance/branch.go`, setting `Fixable = false` when the platform default is neither conventional name (FR-039)
- [X] T050 [US1] Implement the branch-protection check in `internal/compliance/protection.go`, skipping with a stated reason when the branch is absent and never reporting an attribute the platform has no equivalent for (FR-024, FR-026)
- [X] T051 [US1] Assemble the check catalog and the `Report` summary in `internal/compliance/evaluate.go`, which constructs no executor and imports no write path — FR-031 enforced by the import graph
- [X] T052 [US1] Add the `detect` command in `cmd/forgectl/detect.go` — parse, validate, call, format only
- [X] T053 [US1] Add the `check [TYPES...]` command in `cmd/forgectl/check.go`, emitting the "no profile selected, CI variables not checked" warning on stderr when none is selected (FR-019)
- [X] T054 [US1] Add an import-graph test in `internal/compliance/imports_test.go` asserting that `internal/compliance` and `cmd/forgectl/check.go` never reach `internal/apply`, making the read-only guarantee of FR-031 a compile-time property

**Checkpoint**: `forgectl detect` and `forgectl check` are fully functional and testable on their
own. This is the MVP.

---

## Phase 4: User Story 2 — Converge branch and protection settings (Priority: P2)

**Goal**: `apply` shows its plan, asks for confirmation, then renames and pushes the default branch,
sets it on the platform, protects it, and protects the tag patterns the selected profiles declare.
A second run changes nothing.

**Independent Test**: On a repository with default branch `master` and no protection, run apply,
confirm the plan, and verify the platform reports `main` as default and protected. Run apply again
and verify the plan is empty and nothing changes.

### Tests for User Story 2 (MANDATORY — write first, watch them fail) ⚠️

- [X] T055 [P] [US2] Test the branch commands in `internal/gitrepo/gitrepo_test.go` against a real temporary repository with a bare repository as its remote: `git branch -m`, `git branch main origin/master`, `git push -u`, `git remote set-head`, and `git push origin --delete` (R6)
- [X] T056 [P] [US2] Test the protected-tags check in `internal/compliance/tags_test.go`: every pattern the selected profiles declare is protected, and a missing pattern fails (FR-025)
- [X] T057 [P] [US2] Test the GitHub protection write path in `internal/forge/github/github_test.go`: `POST /repos/{o}/{r}/rulesets` and `PUT /repos/{o}/{r}/rulesets/{id}` with `target: branch` and `target: tag`, `enforcement: active`, the `deletion` and `non_fast_forward` rules, and a ruleset carrying any name other than `forgectl` left untouched (R2, research.md open item 2)
- [X] T058 [P] [US2] Test the GitLab protection write path in `internal/forge/gitlab/gitlab_test.go`: `POST` and `PATCH /projects/{id}/protected_branches`, and `GET` and `POST /projects/{id}/protected_tags`
- [X] T059 [P] [US2] Test plan construction and execution in `internal/apply/apply_test.go` against the fake forge: the fixed order branch → protection → tags → vars (FR-034), an empty plan on a compliant repository with zero mutating calls (FR-035, SC-002), `--only` and `--skip` filtering with `tags` inside the `protection` domain, and partial-failure reporting naming what succeeded and what did not (FR-045)
- [X] T060 [P] [US2] Test the four branch state transitions in `internal/apply/branch_test.go`: `master` with no `main` renames, pushes, sets the default, and sets the remote head; both branches present skips the rename and push; the default already `main` is a no-op; any other name fails as not auto-fixable with a manual hint (FR-037–FR-039)
- [X] T061 [P] [US2] End-to-end test in `cmd/forgectl/main_test.go`: the plan preview and the confirmation prompt appear on stderr and never on stdout, declining leaves everything untouched and reports cancellation, `--yes` proceeds, a second run yields an empty plan, and a non-TTY stdin without `--yes` exits `2` (CLI-003, quickstart.md Scenario 4)

### Implementation for User Story 2

- [X] T062 [US2] Implement the branch commands in `internal/gitrepo/gitrepo.go`: rename, create from the remote branch, push with upstream tracking, set the remote head, and delete the old remote branch
- [X] T063 [P] [US2] Implement `SetDefaultBranch`, `SetProtection`, `TagProtection`, and `ProtectTag` on the GitHub client in `internal/forge/github/github.go`, creating and updating only the ruleset named `forgectl` and omitting `non_fast_forward` when `allow_force_push` is configured true
- [X] T064 [P] [US2] Implement `SetDefaultBranch`, `SetProtection`, `TagProtection`, and `ProtectTag` on the GitLab client in `internal/forge/gitlab/gitlab.go`
- [X] T065 [US2] Implement the protected-tags check in `internal/compliance/tags.go`, reporting `Domain: protection` (FR-036)
- [X] T066 [US2] Implement plan construction from the check results in `internal/compliance/plan.go`, each `Action` carrying its domain, a description holding no value, and its `Destructive` flag
- [X] T067 [US2] Implement the plan executor in `internal/apply/apply.go`: fixed domain ordering, `--only`/`--skip` filtering, per-action result capture, and partial-failure reporting that a rerun converges from
- [X] T068 [US2] Implement the branch domain in `internal/apply/branch.go` covering the four transitions, deleting the old remote branch only when explicitly requested and only after the new default is in place (FR-041)
- [X] T069 [US2] Implement the protection and tags domain in `internal/apply/protection.go`
- [X] T070 [US2] Add the `apply [TYPES...]` command in `cmd/forgectl/apply.go` with `--yes`, `--delete-old-branch`, `--only`, `--skip`, the plan preview and confirmation prompt on stderr, `--only` with `--skip` as a usage error, and exit `2` when stdin is not a TTY and `--yes` was not given
- [X] T071 [US2] Emit the post-switch warnings on stderr in `cmd/forgectl/apply.go`: open merge or pull requests targeting the old branch need manual retargeting, and the command other clones must run (FR-040)
- [X] T072 [US2] Render the `actions` array of `output.schema.json` in `internal/report/json.go` and its text equivalent in `internal/report/text.go`, populated only by `apply`

**Checkpoint**: User Stories 1 and 2 both work independently. `apply` converges branch and
protection, and is idempotent.

---

## Phase 5: User Story 3 — Keep CI variables in sync from a profile (Priority: P3)

**Goal**: Every variable the selected profiles declare is checked for presence and
platform-supported attributes, and written from values declared once in configuration — with no
value ever printed.

**Independent Test**: With a profile declaring two variables and configuration providing their
values, run check on a repository that has neither, confirm both report missing, run apply, and
confirm both exist on the platform with the declared attributes — and that no value appeared
anywhere in the output.

### Tests for User Story 3 (MANDATORY — write first, watch them fail) ⚠️

- [X] T073 [P] [US3] Test the resolution chain in `internal/values/resolver_test.go`: `--var-file` beats the inline literal and the `value_ref`, which beat the generator, which yields to a concealed prompt when a TTY is attached; with no TTY and a missing value, apply fails listing every missing name before any change (FR-044)
- [X] T074 [P] [US3] Test `--var-file` loading and refusal in `internal/values/varfile_test.go`: a values file inside the working copy that git does not ignore is refused with no bypass, even under `--allow-insecure-config`, and the refusal precedes any read of a value (FR-056, FR-057, quickstart.md Scenario 7)
- [X] T075 [P] [US3] Test sealed-box encryption in `internal/forge/github/seal_test.go`: a base64 public key decoded to `[32]byte`, sealed with `nacl/box.SealAnonymous`, and the ciphertext base64-encoded (R3)
- [X] T076 [P] [US3] Test the GitHub variable path in `internal/forge/github/github_test.go` with `httptest`: `secret: true` routes to the Actions credential endpoints via `GET .../actions/secrets/public-key` then `PUT .../actions/secrets/{name}`, `secret: false` to `.../actions/variables/{name}`, metadata reads carry no value, and a `404` means absent rather than an error
- [X] T077 [P] [US3] Test the GitLab variable path in `internal/forge/gitlab/gitlab_test.go` with `httptest`: `GET`, `POST`, and `PUT /projects/{id}/variables`, and the masking rejection of R7 — multiline, containing a space, or shorter than 8 characters — each retried exactly once with `masked: false`, warned about by constraint, with `protected` never downgraded (FR-043)
- [X] T078 [P] [US3] Test the variable check in `internal/compliance/variables_test.go`: presence and platform-supported attributes only; a GitLab value differing from the configured one reported as drift; a GitHub write-only credential never compared because `ValueReadable` is false (FR-026, FR-027)
- [X] T079 [P] [US3] Write the confidentiality test in `internal/report/confidentiality_test.go` and `cmd/forgectl/main_test.go`: seed recognisable sentinel values through the config store, a `--var-file`, and a prompt, then assert they appear in no stdout document, no stderr line, no log record, and no error string (FR-054, SC-003)
- [X] T080 [P] [US3] End-to-end test in `cmd/forgectl/main_test.go`: `check <profile>` reports missing variables and exits `3`, `apply <profile> --yes` creates them, and a second `check` exits `0` (quickstart.md Scenario 5)

### Implementation for User Story 3

- [X] T081 [US3] Implement `--var-file` loading and its in-repository refusal in `internal/values/varfile.go`, raised at load time before any value is read
- [X] T082 [US3] Implement the `Resolver` and its ordered chain in `internal/values/resolver.go`, resolving every selected variable to completion before the first write so a missing value cannot leave the repository half-converged
- [X] T083 [US3] Implement the concealed prompt over `golang.org/x/term.ReadPassword` in `internal/values/prompt.go`, writing its prompt to stderr and never echoing or logging the value (R10)
- [X] T084 [P] [US3] Implement sealed-box encryption in `internal/forge/github/seal.go`
- [X] T085 [US3] Implement `Variable` and `SetVariable` on the GitHub client in `internal/forge/github/github.go`, routing on `secret` and always writing on apply because values cannot be read back (FR-042)
- [X] T086 [P] [US3] Implement `Variable` and `SetVariable` on the GitLab client in `internal/forge/gitlab/gitlab.go`, classifying a masking rejection and retrying once unmasked with a warning (R7)
- [X] T087 [US3] Implement the per-variable check in `internal/compliance/variables.go`, emitting ids of the form `vars:<NAME>` and the literal `differs` rather than any differing value
- [X] T088 [US3] Implement the variables domain in `internal/apply/variables.go`, creating missing variables and updating drifted ones
- [X] T089 [US3] Wire `--var-file` into `cmd/forgectl/apply.go` and pass the resolver into the executor

**Checkpoint**: Stories 1–3 all work independently. Variables converge, and no value is ever
disclosed.

---

## Phase 6: User Story 4 — Self-rotating release token (Priority: P4)

**Goal**: A `gitlab-pat` generated variable creates a project access token, writes it straight into
the CI variable, revokes the one it replaces, and thereafter reports the days remaining.

**Independent Test**: Declare a generated variable on a project with no such token, run check and
confirm it reports the token missing, run apply, then confirm exactly one active token with the
configured name exists, the CI variable is set, and a second check passes reporting the remaining
lifetime.

### Tests for User Story 4 (MANDATORY — write first, watch them fail) ⚠️

- [X] T090 [P] [US4] Test the token lifecycle in `internal/forge/gitlab/token_test.go` with `httptest`: `GET`, `POST`, and `DELETE /projects/{id}/access_tokens`; `expires_at` sent explicitly as `YYYY-MM-DD`; `access_level` `30` and `40`; the token value present only in the creation response; and the instance-maximum error `The expiration date must be within the allowed lifetime.` surfaced verbatim (R8, FR-052)
- [X] T091 [P] [US4] Test the generated-variable check in `internal/compliance/generator_test.go`: no token fails "token missing"; two tokens of that name fail as ambiguous recommending rotation; a remaining lifetime at or below `rotate_before` fails stating the days remaining; a missing CI variable fails; otherwise it passes carrying `expires_at` and `rotate_in_days` (FR-028)
- [X] T092 [P] [US4] Test the rotation sequence in `internal/apply/token_test.go` against the fake forge: create, write the variable, then revoke — never the other way round — leaving exactly one active token (FR-047–FR-049); a variable write that fails after creation reports that the token remains active and apply must be rerun (FR-051); `--force-rotate` rotates a healthy token (FR-053)
- [X] T093 [P] [US4] Test the two skip paths in `internal/compliance/generator_test.go`: a generated variable on a GitHub instance is skipped with a warning and does not fail the run (FR-029), and a `403` on token listing skips with that reason rather than counting as drift (FR-030)
- [X] T094 [P] [US4] End-to-end test in `cmd/forgectl/main_test.go`: `check go-release` exits `3` with "token missing", `apply go-release --yes` converges, a second `check` exits `0`, and `--output=json` carries the generator's expiry and remaining days (FR-055, quickstart.md Scenario 6)

### Implementation for User Story 4

- [X] T095 [US4] Implement `ProjectTokens`, `CreateProjectToken`, and `RevokeProjectToken` in `internal/forge/gitlab/token.go`, the creation returning the token value as a second return that is passed on and never stored (FR-050)
- [X] T096 [US4] Implement the generated-variable check in `internal/compliance/generator.go`, populating `GeneratorStatus` with the kind, expiry, and days remaining
- [X] T097 [US4] Implement the rotation sequence in `internal/apply/token.go`: create, write the CI variable immediately, then revoke every other token of that name only after the write succeeds
- [X] T098 [US4] Add `--force-rotate` to `cmd/forgectl/apply.go` and thread it into the plan as a destructive action requiring confirmation (CLI-003)
- [X] T099 [US4] Render `generator` — kind, `expires_at`, `rotate_in_days` — in `internal/report/json.go` and `internal/report/text.go` (FR-055)

**Checkpoint**: Stories 1–4 all work independently. Release tokens rotate themselves on GitLab and
are skipped cleanly on GitHub.

---

## Phase 7: User Story 5 — Discover what a profile does (Priority: P5)

**Goal**: List the available profiles and inspect one in detail — variables, attributes,
value-source kinds, and protected tag patterns — with no value revealed.

**Independent Test**: With no configuration file at all, list the profiles and confirm the three
built-in ones appear; add a configuration overriding one and confirm the listing reflects the
override rather than duplicating the name.

### Tests for User Story 5 (MANDATORY — write first, watch them fail) ⚠️

- [X] T100 [P] [US5] Test the listing and detail views in `internal/report/profiles_test.go`: with no configuration the three built-ins appear; a configured profile of a built-in name appears once reflecting the configured definition; the detail view shows each variable's name, attributes, and value-source kind plus the profile's protected tag patterns, and shows no value (FR-020)
- [X] T101 [P] [US5] End-to-end test in `cmd/forgectl/main_test.go` with `HOME` pointed at an empty directory: `profiles list` and `profiles show go-release` succeed with no configuration file present and disclose no value (quickstart.md Scenario 8, SC-009)

### Implementation for User Story 5

- [X] T102 [US5] Add the `profiles list` and `profiles show TYPE` commands in `cmd/forgectl/profiles.go`, read-only and never prompting, with an unknown profile name exiting `2`
- [X] T103 [US5] Render the profile listing and detail in `internal/report/profiles.go` for both `--output=text` and `--output=json`, emitting the value-source kind and never a value

**Checkpoint**: All five user stories are independently functional.

---

## Phase 8: Polish & Cross-Cutting Concerns

- [X] T104 [P] Add the `version` command in `cmd/forgectl/version.go`, reading `runtime/debug.ReadBuildInfo`
- [X] T105 [P] State the exit codes `0`/`1`/`2`/`3`, the `flags > environment > config file > defaults` precedence, and the `git`-on-`PATH` requirement in the `--help` text in `cmd/forgectl/main.go` (CLI-002, CLI-004, Constitution V, R6)
- [X] T106 [P] Write `README.md` documenting the commands, the exit codes, `--output`, the configuration precedence, the `git` runtime requirement, and the confidentiality guarantee (constitution Sync Impact Report follow-up)
- [X] T107 Refactor — the R of red-green-refactor — with every test still green, checking that no `utils`/`helpers`/`common`/`base` package appeared, that every error is wrapped with `%w`, and that no type parameter was introduced (Constitution III, IV)
- [X] T108 `task lint` passes with no findings from golangci-lint v2
- [X] T109 `task test` passes: `go test -count=2 -race ./...`
- [X] T110 `govulncheck ./...` reports no known vulnerabilities and `go generate ./...` produces no diff
- [X] T111 [P] Verify the static build: `task build`, then `go version -m ./dist/forgectl` shows `CGO_ENABLED=0` and `-trimpath` (Constitution II, quickstart.md)
- [X] T112 [P] Measure a ten-variable profile check against a responsive `httptest` forge and confirm it completes in under ten seconds, in `internal/compliance/perf_test.go` (SC-008)
- [X] T113 Test interruption in `cmd/forgectl/main_test.go`: `SIGINT` during apply stops at the current step, reports what completed, and exits without a panic or a stack trace, and a rerun converges (CLI-005, SC-007, quickstart.md Scenario 9)
- [X] T114 Walk every scenario in `specs/001-forge-conventions/quickstart.md` and record the result, including the live GitLab and GitHub scenarios

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — starts immediately
- **Foundational (Phase 2)**: Depends on Setup — **BLOCKS every user story**
- **User Stories (Phases 3–7)**: All depend on Foundational; then either in parallel if staffed, or
  sequentially in priority order P1 → P2 → P3 → P4 → P5
- **Polish (Phase 8)**: Depends on every story being shipped

### User Story Dependencies

- **US1 (P1)**: Depends only on Foundational. No dependency on any other story.
- **US2 (P2)**: Depends only on Foundational. It executes the plan `internal/compliance` builds, so
  sharing a developer with US1 is convenient, but its own tests use the fake forge and stand alone.
- **US3 (P3)**: Depends only on Foundational. Independent of US1 and US2.
- **US4 (P4)**: Depends on **US3** — a generated variable is a variable, so the variable check and
  the variable write must exist first. This is the one real cross-story dependency.
- **US5 (P5)**: Depends only on Foundational. Independent of every other story.

### Within Each User Story

- Tests are written first and MUST fail before the implementation lands (Constitution VII)
- Domain types before operations; operations in `internal/<domain>/` before the wrapper in
  `cmd/forgectl/`
- The GitHub and GitLab clients are always parallelisable with each other — different directories

### Parallel Opportunities

- Setup: T003–T006 together
- Foundational: the four groups — CLI shell (T007–T011), gitrepo (T012–T017), config (T018–T028),
  forge (T029–T034) — are independent of one another and can be staffed in parallel; the shared
  result document (T035–T040) needs only the compliance types
- Every task marked [P] inside a phase touches a distinct file
- All tests inside a story phase can be written in parallel before any implementation begins
- US1, US2, US3, and US5 can proceed in parallel once Foundational is done; US4 waits on US3

---

## Parallel Example: User Story 1

```bash
# Write all six failing tests together, before any implementation:
Task: "Default-branch check test in internal/compliance/branch_test.go"
Task: "Branch-protection check test in internal/compliance/protection_test.go"
Task: "GitHub read path httptest in internal/forge/github/github_test.go"
Task: "GitLab read path httptest in internal/forge/gitlab/gitlab_test.go"
Task: "End-to-end detect and check in cmd/forgectl/main_test.go"
Task: "Subdirectory discovery end to end in cmd/forgectl/main_test.go"

# Then the two platform clients together — different directories:
Task: "GitHub read methods in internal/forge/github/github.go"
Task: "GitLab read methods in internal/forge/gitlab/gitlab.go"
```

## Parallel Example: Foundational

```bash
# Four independent groups, one per developer:
Task: "CLI shell — T007 through T011 in cmd/forgectl/main.go"
Task: "Local working copy — T012 through T017 in internal/gitrepo/"
Task: "Configuration — T018 through T028 in internal/config/"
Task: "Forge abstraction — T029 through T034 in internal/forge/"
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1: Setup — T001–T006
2. Phase 2: Foundational — T007–T040 (**critical, blocks everything**)
3. Phase 3: User Story 1 — T041–T054
4. **STOP and VALIDATE**: quickstart.md Scenarios 1, 2, and 3 pass. `detect` and `check` are useful
   on their own — a maintainer with a dozen repositories already gets the audit
5. Ship

### Incremental Delivery

1. Setup + Foundational → the shell finds the repository and loads configuration
2. **+ US1** → read-only audit of branch and protection, exit `3` on drift (MVP)
3. **+ US2** → `apply` converges branch, protection, and tags, idempotently
4. **+ US3** → CI variables from profiles, with no value disclosed
5. **+ US4** → self-rotating GitLab release tokens
6. **+ US5** → profile discovery
7. Polish → docs, gates, quickstart walk-through

Each increment adds value without breaking the one before it.

### Parallel Team Strategy

1. The team completes Setup together, then splits Foundational across its four groups
2. Once Foundational is green:
   - Developer A: US1, then US2 (they share the compliance and forge surfaces most heavily)
   - Developer B: US3, then US4 (US4 depends on US3)
   - Developer C: US5, then Polish
3. Stories integrate independently; only US4's dependency on US3 constrains the ordering

---

## Notes

- [P] means a different file and no dependency on an incomplete task
- The [Story] label maps a task to its user story for traceability
- Verify each test fails before implementing it — a new test that passes proves nothing
  (Constitution VII)
- Commit after each task or logical group
- Two design decisions correct the source specification and must not be reverted: GitHub protection
  goes through **rulesets**, never the sunset tag-protection API (R2), and the unmasked retry keys
  on the **class of masking rejection**, not on multiline detection alone (R7)
- No value ever reaches `internal/report`, a log record, or an error string — a value lives only in
  `internal/values` and in the argument to `forge.SetVariable` (FR-054)
