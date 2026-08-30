# Quickstart: Validating Forge Convention Check & Apply

**Feature**: `specs/001-forge-conventions` · **Date**: 2026-08-30

How to prove the feature works, end to end. Each scenario maps to a user story and its independent
test in [spec.md](./spec.md). Details live in [contracts/cli.md](./contracts/cli.md),
[data-model.md](./data-model.md), and [contracts/forge-endpoints.md](./contracts/forge-endpoints.md)
— this file does not repeat them.

---

## Prerequisites

- Go 1.26.1 (`mise install`), Task, golangci-lint v2, and `git` on `PATH`
- For the offline scenarios: nothing else
- For the live scenarios: a throwaway repository on a GitLab instance and one on GitHub, plus a
  credential with `api` scope and Maintainer on the GitLab project

## Build and gate

```bash
task lint                  # golangci-lint v2, no findings
task test                  # go test -count=2 -race ./...
govulncheck ./...          # no known vulnerabilities
go generate ./...          # must produce no diff
task build                 # CGO_ENABLED=0, -trimpath
```

All four gates must pass before a change is complete (Constitution: Development Workflow).

Confirm the binary is genuinely static:

```bash
go version -m ./dist/forgectl | grep -E 'CGO_ENABLED|-trimpath'
# expect CGO_ENABLED=0 and -trimpath
```

---

## Scenario 1 — Audit a drifted repository (US1, offline)

Proves detection, the read-only guarantee, and the drift exit code.

```bash
# a throwaway working copy whose default branch is master
git init -b master /tmp/qs && cd /tmp/qs
git commit --allow-empty -m init
git remote add origin git@gitlab.example.com:acme/my-tool.git

forgectl detect
# expect: repository acme/my-tool, instance, platform gitlab, API base URL

forgectl check; echo "exit=$?"
# expect: branch check FAILS, expected "main", found "master"
# expect: exit=3   (drift found)
```

**Then verify nothing changed**: `git log`, `git branch -a`, and the platform's settings are all
untouched. `check` never imports `internal/apply`, so this is guaranteed by the import graph, but
verify it once by hand anyway.

```bash
forgectl check --output=json | jq -e '.summary.fail > 0 and (.checks[0].id == "branch")'
forgectl check --output=json | jq .        # must be a clean document, no prompt text mixed in
```

## Scenario 2 — The stream split (US1, offline)

Proves CLI-001, the single most important property for pipe safety.

```bash
forgectl check --output=json 1>/tmp/out.json 2>/tmp/err.log
jq . /tmp/out.json                  # must parse: stdout is data only
grep -q . /tmp/err.log && cat /tmp/err.log   # warnings and logs live here
```

With no profile selected, the "no profile provided, CI variables not checked" warning (FR-019) must
appear in `/tmp/err.log` and **not** in `/tmp/out.json`.

```bash
forgectl check > /dev/null            # stdout not a TTY
# expect: no colour codes, no spinner, no progress bar anywhere
NO_COLOR=1 forgectl check             # expect: no colour even on a TTY
```

## Scenario 3 — Exit codes (all four, offline)

Proves CLI-002. Constitution V requires each to be covered by a test; this is the manual mirror.

```bash
forgectl check;                      echo "compliant  -> $?"   # 0
forgectl check;                      echo "drifted    -> $?"   # 3
forgectl --bogus-flag check;         echo "usage      -> $?"   # 2
cd /tmp && forgectl check;           echo "not a repo -> $?"   # 2
FORGECTL_GITLAB_PERSONAL= forgectl check; echo "no cred -> $?" # 2
# runtime failure -> 1: point --config at an instance whose api_url is unreachable
```

## Scenario 4 — Converge branch and protection (US2, live)

Proves the plan preview, the confirmation gate, and idempotency.

```bash
forgectl apply
# expect on stderr: the plan, then "Confirm? [y/N]"
# answer n: nothing changes, command reports cancellation

forgectl apply --yes
# expect: main pushed and set as default; main protected

forgectl apply --yes
# expect: empty plan, no confirmation, no mutating call -> the idempotency proof (SC-002)
forgectl check; echo "exit=$?"     # expect 0
```

Verify on the platform that the default branch is `main`, that force-push and deletion are denied,
and — on GitHub — that a ruleset named `forgectl` exists with `target: branch` (R2: the old tag
protection API would have appeared to succeed while protecting nothing).

Check the warnings that FR-040 requires: open merge or pull requests targeting the old branch need
manual retargeting, and the command other clones must run is printed.

## Scenario 5 — CI variables from a profile (US3, live)

```bash
forgectl check ansible-role; echo "exit=$?"   # expect 3, GALAXY_API_TOKEN missing
forgectl apply ansible-role --yes
forgectl check ansible-role; echo "exit=$?"   # expect 0
```

Confirm on the platform that the variable exists with the declared attributes. Then the
confidentiality check, which is the whole point of SC-003:

```bash
forgectl check ansible-role --output=json 2>/tmp/err.log | tee /tmp/out.json
grep -F "$KNOWN_VALUE" /tmp/out.json /tmp/err.log && echo "LEAK — FAIL" || echo "no value disclosed"
```

Multiline handling (FR-043, R7): apply `ssh-deploy`, whose `SSH_PRIVATE_KEY` is multiline and
therefore cannot be masked. Expect the write to be retried unmasked with a warning on stderr naming
the constraint — and expect it to succeed, not fail.

## Scenario 6 — Generated, self-rotating token (US4, live, GitLab only)

```bash
forgectl check go-release; echo "exit=$?"     # expect 3, "token missing"
forgectl apply go-release --yes
forgectl check go-release; echo "exit=$?"     # expect 0
forgectl check go-release --output=json | jq '.checks[] | select(.generator)'
# expect: expires_at ~180 days out, rotate_in_days ~120
```

Verify in the GitLab project settings that **exactly one** active token named `forgectl` exists
(spec §7.1 invariant), and that `GITLAB_TOKEN` is set.

```bash
forgectl apply go-release --yes --force-rotate
# expect: a new token created, the previous one revoked, still exactly one active
```

On a GitHub repository, the same profile must **skip** the generated variable with a warning and
still exit 0 if nothing else drifts (FR-029) — never fail.

## Scenario 7 — Values must not sit in the repository (FR-056, offline)

```bash
cd /tmp/qs
printf 'values:\n  galaxy: "test-value"\n' > vars.yaml
forgectl apply ansible-role --var-file vars.yaml --yes; echo "exit=$?"
# expect exit=2, naming vars.yaml and the .gitignore line to add
# expect the refusal BEFORE any value is read and before any platform call (FR-057)

forgectl apply ansible-role --var-file vars.yaml --yes --allow-insecure-config; echo "exit=$?"
# expect exit=2 still — this refusal has no bypass

echo 'vars.yaml' >> .gitignore
forgectl apply ansible-role --var-file vars.yaml --yes; echo "exit=$?"
# expect it to proceed, with no warning: an ignored file cannot be committed
```

Also confirm the permission gate of FR-007:

```bash
chmod 0644 ~/.config/forgectl/config.yaml
forgectl check; echo "exit=$?"    # expect 2, naming the file, its mode, and the chmod to run
forgectl check --allow-insecure-config; echo "exit=$?"   # expect it to proceed
chmod 0600 ~/.config/forgectl/config.yaml
```

## Scenario 8 — Discover profiles (US5, offline, no config at all)

```bash
mv ~/.config/forgectl/config.yaml{,.bak}
forgectl profiles list                # expect ansible-role, go-release, ssh-deploy (embedded)
forgectl profiles show go-release     # expect variables, attributes, value-source kinds,
                                      # protected_tags — and NO values
mv ~/.config/forgectl/config.yaml{.bak,}
```

## Scenario 9 — Interruption and cancellation (CLI-005)

```bash
forgectl apply go-release --yes &     # then Ctrl-C, or: kill -INT %1
# expect: stops at the current step, reports what completed, no panic, no stack trace
forgectl apply go-release --yes       # expect: converges from wherever it stopped (FR-045)
```

If the interruption lands between creating a token and writing its variable, the report must say so
explicitly (FR-051) — that token cannot be recovered, only rotated again.

---

## Definition of done for this feature

- Every scenario above behaves as described
- `task lint`, `task test`, `govulncheck ./...` pass; `go generate ./...` produces no diff
- The end-to-end test in `cmd/forgectl/main_test.go` covers all four exit codes and the
  stdout/stderr split against the built binary (Constitution VII)
- The confidentiality test seeds sentinel values through every path and asserts they appear in no
  stream, no log line, and no error string (SC-003)
