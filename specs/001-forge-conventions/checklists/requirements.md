# Specification Quality Checklist: Forge Convention Check & Apply

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-08-30
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs)
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable
- [x] Success criteria are technology-agnostic (no implementation details)
- [x] All acceptance scenarios are defined
- [x] Edge cases are identified
- [x] Scope is clearly bounded
- [x] Dependencies and assumptions identified

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification

## Notes

**Iteration 1 (2026-08-30)** — one item failed: three [NEEDS CLARIFICATION] markers, each raised
because the source specification and the project constitution gave different answers, or because
the source specification contradicted itself.

**Iteration 2 (2026-08-30)** — all items pass. The three questions were put to the author and
answered; the answers are written into the requirements and recorded under *Resolved conflicts* in
the spec's Assumptions section:

1. **Exit codes (CLI-002)** — the constitution's `0`/`1`/`2` semantics stand unchanged and drift
   takes a fourth code, `3`. No constitution amendment needed. `2` now means "nothing was
   attempted", `1` means "work began and broke", which is a mechanically testable split.
2. **Variable scoping (FR-032)** — one run touches exactly one instance, the one hosting the
   detected repository; a variable definition carries no platform or instance field. The
   cross-instance rows in the source specification's sample output are dropped as inconsistent with
   its own schema.
3. **Values inside the working copy (FR-056, FR-057)** — a values-bearing file that git does not
   ignore is a refusal with no bypass; the same file, ignored, is accepted. The per-repository
   override workflow survives, and a value that cannot be committed is not in the repository.

Notes on items that pass but are worth recording:

- *No implementation details* — GitHub and GitLab are named throughout. They are the product's
  scope, not a technology choice, and naming them is what makes FR-024, FR-026, FR-027, and FR-029
  testable. No library, language, endpoint path, or wire format is named; the source
  specification's candidate libraries were deliberately dropped and recorded in Assumptions as a
  planning-phase decision requiring the author's approval under the constitution's Dependencies &
  Supply Chain section.
- *Written for non-technical stakeholders* — the domain is developer tooling, so branch protection
  and CI variables are the stakeholder's own vocabulary. No code-level concept appears.
- *Scope is clearly bounded* — the source specification's v1 non-goals and roadmap are carried into
  the final Assumptions entry.

**Two items for the planning phase**, both recorded in the spec's Assumptions rather than left as
open questions:

- GitHub's tag protection capability must be verified to still exist; if it is gone, protected tags
  are GitLab-only and GitHub repositories report that check as a skip with a stated reason.
- Every third-party library is unchosen. The constitution requires the author's prior approval for
  each direct dependency, so `/speckit-plan` must propose them explicitly rather than assume the
  candidates named in the source specification.

Checklist complete — ready for `/speckit-plan`.
