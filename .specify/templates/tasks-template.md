---

description: "Task list template for feature implementation"
---

# Tasks: [FEATURE NAME]

**Input**: Design documents from `/specs/[###-feature-name]/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/

**Tests**: Test tasks are MANDATORY. Constitution VII (Test-First, Black Box) is non-negotiable: every behaviour change arrives with the test that fails without it, so each user story below opens with its failing tests.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

- **Command wrapper**: `cmd/forgectl/` - parse, validate, call, format only (Constitution III)
- **Business logic**: `internal/<domain>/` - MUST import no CLI package
- **Tests**: `internal/<domain>/<domain>_test.go` in `package <domain>_test`; internals bridged
  through `internal/<domain>/export_test.go` (Constitution VII)
- **Golden files**: `internal/<domain>/testdata/`
- `utils`, `helpers`, `common`, and `base` packages are forbidden - name the package for its domain
- Adjust `<domain>` to the concrete packages named in plan.md

<!--
  ============================================================================
  IMPORTANT: The tasks below are SAMPLE TASKS for illustration purposes only.

  The /speckit-tasks command MUST replace these with actual tasks based on:
  - User stories from spec.md (with their priorities P1, P2, P3...)
  - Feature requirements from plan.md
  - Entities from data-model.md
  - Endpoints from contracts/

  Tasks MUST be organized by user story so each story can be:
  - Implemented independently
  - Tested independently
  - Delivered as an MVP increment

  DO NOT keep these sample tasks in the generated tasks.md file.
  ============================================================================
-->

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and basic structure

- [ ] T001 Create the package layout from plan.md (`cmd/forgectl/`, `internal/<domain>/`)
- [ ] T002 Initialize `go.mod` with pinned `go` and `toolchain` directives matching `mise.toml`
- [ ] T003 [P] Configure `.golangci.yml`, `Taskfile.yml` (`lint`, `test`, `release`), and
      `.goreleaser.yml` (`CGO_ENABLED=0`, `-trimpath`, SHA-256 checksums, SBOM)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

Examples of foundational tasks (adjust based on your project):

- [ ] T004 Wire exit-code mapping in `cmd/forgectl/main.go`: 0 success, 1 runtime failure,
      2 usage error - cobra sets none of its own
- [ ] T005 [P] Set up the root command with `--output=text|json`, `--quiet`, `--verbose`, `--yes`
- [ ] T006 [P] Implement config precedence flags > environment > config file > defaults, and
      state it in `--help`
- [ ] T007 Create the core domain types in `internal/<domain>/` that all stories depend on
- [ ] T008 Configure `log/slog` to stderr at a user-controlled level; honour `NO_COLOR` and emit
      no colour, spinners, or progress bars when stdout is not a TTY
- [ ] T009 Set up the root `context.Context`, cancelled on `SIGINT`/`SIGTERM`, with explicit I/O
      timeouts and bounded, backed-off retries

**Checkpoint**: Foundation ready - user story implementation can now begin in parallel

---

## Phase 3: User Story 1 - [Title] (Priority: P1) 🎯 MVP

**Goal**: [Brief description of what this story delivers]

**Independent Test**: [How to verify this story works on its own]

### Tests for User Story 1 (MANDATORY - write first, watch them fail) ⚠️

> **NOTE: Write these tests FIRST, ensure they FAIL before implementation**

- [ ] T010 [P] [US1] Black-box test for [behaviour] in internal/[domain]/[domain]_test.go
      (`package [domain]_test`)
- [ ] T011 [P] [US1] End-to-end test in cmd/forgectl/main_test.go invoking the built binary for
      [user journey], asserting exit code and the stdout/stderr split

### Implementation for User Story 1

- [ ] T012 [P] [US1] Create [Type1] in internal/[domain]/[type1].go
- [ ] T013 [P] [US1] Create [Type2] in internal/[domain]/[type2].go
- [ ] T014 [US1] Implement [operation] in internal/[domain]/[operation].go (depends on T012, T013)
- [ ] T015 [US1] Add the thin command wrapper in cmd/forgectl/[command].go - parse, validate,
      call, format
- [ ] T016 [US1] Add validation and wrap every error with `fmt.Errorf("...: %w", err)`
- [ ] T017 [US1] Add `log/slog` records on stderr for user story 1 operations

**Checkpoint**: At this point, User Story 1 should be fully functional and testable independently

---

## Phase 4: User Story 2 - [Title] (Priority: P2)

**Goal**: [Brief description of what this story delivers]

**Independent Test**: [How to verify this story works on its own]

### Tests for User Story 2 (MANDATORY - write first, watch them fail) ⚠️

- [ ] T018 [P] [US2] Black-box test for [behaviour] in internal/[domain]/[domain]_test.go
- [ ] T019 [P] [US2] End-to-end test for [user journey] in cmd/forgectl/main_test.go

### Implementation for User Story 2

- [ ] T020 [P] [US2] Create [Type] in internal/[domain]/[type].go
- [ ] T021 [US2] Implement [operation] in internal/[domain]/[operation].go
- [ ] T022 [US2] Add the command wrapper in cmd/forgectl/[command].go
- [ ] T023 [US2] Integrate with User Story 1 components (if needed)

**Checkpoint**: At this point, User Stories 1 AND 2 should both work independently

---

## Phase 5: User Story 3 - [Title] (Priority: P3)

**Goal**: [Brief description of what this story delivers]

**Independent Test**: [How to verify this story works on its own]

### Tests for User Story 3 (MANDATORY - write first, watch them fail) ⚠️

- [ ] T024 [P] [US3] Black-box test for [behaviour] in internal/[domain]/[domain]_test.go
- [ ] T025 [P] [US3] End-to-end test for [user journey] in cmd/forgectl/main_test.go

### Implementation for User Story 3

- [ ] T026 [P] [US3] Create [Type] in internal/[domain]/[type].go
- [ ] T027 [US3] Implement [operation] in internal/[domain]/[operation].go
- [ ] T028 [US3] Add the command wrapper in cmd/forgectl/[command].go

**Checkpoint**: All user stories should now be independently functional

---

[Add more user story phases as needed, following the same pattern]

---

## Phase N: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories

- [ ] TXXX [P] Document exit codes, `--output`, and config precedence in `--help` and README.md
- [ ] TXXX Refactor - the R of red-green-refactor, with the tests still green
- [ ] TXXX `task lint` passes (golangci-lint v2, no findings)
- [ ] TXXX `task test` passes (`go test -count=2 -race ./...`)
- [ ] TXXX `govulncheck ./...` passes and `go generate ./...` produces no diff
- [ ] TXXX Verify no credentials or tokens reach the repository, logs, or error messages
- [ ] TXXX Run quickstart.md validation

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **User Stories (Phase 3+)**: All depend on Foundational phase completion
  - User stories can then proceed in parallel (if staffed)
  - Or sequentially in priority order (P1 → P2 → P3)
- **Polish (Final Phase)**: Depends on all desired user stories being complete

### User Story Dependencies

- **User Story 1 (P1)**: Can start after Foundational (Phase 2) - No dependencies on other stories
- **User Story 2 (P2)**: Can start after Foundational (Phase 2) - May integrate with US1 but should be independently testable
- **User Story 3 (P3)**: Can start after Foundational (Phase 2) - May integrate with US1/US2 but should be independently testable

### Within Each User Story

- Tests MUST be written and MUST FAIL before implementation (Constitution VII)
- Domain types before operations
- Operations in `internal/<domain>/` before the command wrapper in `cmd/forgectl/`
- Core implementation before integration
- Story complete before moving to next priority

### Parallel Opportunities

- All Setup tasks marked [P] can run in parallel
- All Foundational tasks marked [P] can run in parallel (within Phase 2)
- Once Foundational phase completes, all user stories can start in parallel (if team capacity allows)
- All tests for a user story marked [P] can run in parallel
- Domain types within a story marked [P] can run in parallel
- Different user stories can be worked on in parallel by different team members

---

## Parallel Example: User Story 1

```bash
# Launch all tests for User Story 1 together (they MUST fail first):
Task: "Black-box test for [behaviour] in internal/[domain]/[domain]_test.go"
Task: "End-to-end binary test for [user journey] in cmd/forgectl/main_test.go"

# Launch all domain types for User Story 1 together:
Task: "Create [Type1] in internal/[domain]/[type1].go"
Task: "Create [Type2] in internal/[domain]/[type2].go"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL - blocks all stories)
3. Complete Phase 3: User Story 1
4. **STOP and VALIDATE**: Test User Story 1 independently
5. Deploy/demo if ready

### Incremental Delivery

1. Complete Setup + Foundational → Foundation ready
2. Add User Story 1 → Test independently → Deploy/Demo (MVP!)
3. Add User Story 2 → Test independently → Deploy/Demo
4. Add User Story 3 → Test independently → Deploy/Demo
5. Each story adds value without breaking previous stories

### Parallel Team Strategy

With multiple developers:

1. Team completes Setup + Foundational together
2. Once Foundational is done:
   - Developer A: User Story 1
   - Developer B: User Story 2
   - Developer C: User Story 3
3. Stories complete and integrate independently

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story should be independently completable and testable
- Verify tests fail before implementing - a passing new test proves nothing (Constitution VII)
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
- Avoid: vague tasks, same file conflicts, cross-story dependencies that break independence
