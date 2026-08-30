# Contract: Forge Endpoints

**Feature**: `specs/001-forge-conventions` · **Date**: 2026-08-30

Every platform call forgectl makes. Each is reached through the official client (R5) but carries
forgectl's own transport: a 30-second per-request timeout, `context.Context` propagation, and at
most 3 retries with exponential backoff (CLI-005). This list is what the `httptest` suites mock.

---

## GitHub

Client: `github.com/google/go-github/v90`.

| Purpose | Call | Requirement |
|---|---|---|
| Read the default branch | `GET /repos/{o}/{r}` | FR-023 |
| Set the default branch | `PATCH /repos/{o}/{r}` — `{"default_branch": "main"}` | FR-037, FR-038 |
| Does a branch exist | `GET /repos/{o}/{r}/branches/{branch}` | FR-024 |
| Read protection | `GET /repos/{o}/{r}/rulesets` then `GET /repos/{o}/{r}/rulesets/{id}` | FR-024, FR-025 |
| Write protection | `POST /repos/{o}/{r}/rulesets` / `PUT /repos/{o}/{r}/rulesets/{id}` | FR-037, FR-041 |
| Actions secret public key | `GET /repos/{o}/{r}/actions/secrets/public-key` | FR-042 |
| Write an Actions secret | `PUT /repos/{o}/{r}/actions/secrets/{name}` — `encrypted_value`, `key_id` | FR-042 |
| Read an Actions secret's metadata | `GET /repos/{o}/{r}/actions/secrets/{name}` | FR-026 |
| Write an Actions variable | `PUT` / `POST /repos/{o}/{r}/actions/variables/{name}` | FR-042 |
| Read an Actions variable | `GET /repos/{o}/{r}/actions/variables/{name}` | FR-026, FR-027 |

### Rulesets replace tag protection

**The `/repos/{o}/{r}/tags/protection` API named in spec §6.3 is sunset** and has returned NULL
data since 2024-08-30 (R2). Calling it appears to succeed while protecting nothing — unacceptable
for a tool whose job is verifying protection. Both branch and tag protection go through rulesets.

Branch protection body:

```json
{
  "name": "forgectl",
  "target": "branch",
  "enforcement": "active",
  "conditions": { "ref_name": { "include": ["refs/heads/main"], "exclude": [] } },
  "rules": [{ "type": "deletion" }, { "type": "non_fast_forward" }]
}
```

Tag protection body: identical with `"target": "tag"` and
`"include": ["refs/tags/v*"]`.

`deletion` denies deletion, `non_fast_forward` denies force-push. Omitting a rule permits the
action, so `allow_force_push: true` in config means omitting `non_fast_forward`.

**Ownership**: forgectl creates rulesets named `forgectl` and modifies only those. A ruleset with
any other name is left untouched and, if it already grants the required protection, the check still
passes — forgectl verifies the effect, not its own authorship.

### Secret encryption

`public-key` returns a base64 key and a `key_id`. Decode to `[32]byte`, seal with
`nacl/box.SealAnonymous`, base64 the ciphertext, send it with the `key_id`.

**Values are write-only.** `GET` on a secret returns metadata only — never the value — so FR-027
never compares and FR-042 writes on every apply.

### Attribute mapping

`secret: true` → Actions secret. `secret: false` → Actions variable. `masked` and `protected` have
no GitHub equivalent and are never reported as drift (FR-026).

**Generated variables are skipped with a warning on GitHub** (FR-029): there is no project access
token equivalent.

---

## GitLab

Client: `gitlab.com/gitlab-org/api/client-go` — the maintained successor to the archived
`github.com/xanzy/go-gitlab` (R5).

| Purpose | Call | Requirement |
|---|---|---|
| Read the default branch | `GET /projects/{id}` | FR-023 |
| Set the default branch | `PUT /projects/{id}` — `{"default_branch": "main"}` | FR-037, FR-038 |
| Does a branch exist | `GET /projects/{id}/repository/branches/{branch}` | FR-024 |
| Read branch protection | `GET /projects/{id}/protected_branches/{name}` | FR-024 |
| Protect a branch | `POST /projects/{id}/protected_branches` | FR-037 |
| Update branch protection | `PATCH /projects/{id}/protected_branches/{name}` | FR-037 |
| Read protected tags | `GET /projects/{id}/protected_tags` | FR-025 |
| Protect a tag pattern | `POST /projects/{id}/protected_tags` — `{"name": "v*"}` | FR-025 |
| Read a CI variable | `GET /projects/{id}/variables/{key}` | FR-026, FR-027 |
| Create a CI variable | `POST /projects/{id}/variables` | FR-042 |
| Update a CI variable | `PUT /projects/{id}/variables/{key}` | FR-042 |
| List project access tokens | `GET /projects/{id}/access_tokens` | FR-028 |
| Create a project access token | `POST /projects/{id}/access_tokens` | FR-047 |
| Revoke a project access token | `DELETE /projects/{id}/access_tokens/{token_id}` | FR-048 |

The project id is `owner/repo`, URL-encoded.

### Protection semantics

`allow_force_push` is a boolean. `push_access_level` takes `0` none, `30` developer, `40`
maintainer.

**Deleting a protected branch is always denied by GitLab** — there is no toggle (R9). The config's
`allow_delete: false` is therefore satisfied by the branch being protected at all, and MUST NOT be
reported as drift.

### Masked variable constraints

GitLab rejects a masked value that is **multiline, contains a space, or is shorter than 8
characters** — three constraints, not the one named in spec §6.4 (R7). A multiline value returns
`Value must be a single line.`

On any masking rejection, apply retries **once** with `masked: false` and warns on stderr naming
the constraint. `protected` is never downgraded.

### Project access tokens

`POST` body: `name`, `scopes`, `access_level` (30 developer, 40 maintainer), `expires_at` as
`YYYY-MM-DD`. forgectl always sends `expires_at` explicitly rather than relying on the instance
default.

**The token value is returned only in the creation response.** There is no way to read it back,
which is why the CI variable write must follow immediately (FR-047) and why a failure between the
two is unrecoverable except by rotating again (FR-051).

Instance maximum lifetime is 365 days by default and administrator-configurable. Exceeding it
returns `The expiration date must be within the allowed lifetime.`, surfaced verbatim (FR-052).

The operating credential needs `api` scope and Maintainer or Owner on the project. Lacking the
right to list or create tokens is a **skip with reason**, never a failure (FR-030).

---

## Shared transport behaviour

| Condition | Behaviour |
|---|---|
| Any request | 30-second timeout, context-cancellable |
| `429`, or `5xx` | Retry, bounded at 3 attempts, exponential backoff (FR-046) |
| `401`, `403` on a credential | Runtime failure, exit `1`, message names the instance and the environment variable — never the credential |
| `403` on token listing | Skip with reason (FR-030) |
| `404` on a variable or branch | Not an error: reported as absent |
| Any error message | Passed through `%w` and rendered without any value (FR-054) |
