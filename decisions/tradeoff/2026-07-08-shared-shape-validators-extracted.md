# Shared shape-validators extracted from normalize and reused by the edit path

| Field    | Value            |
|----------|------------------|
| Date     | 2026-07-08       |
| Status   | accepted         |
| Category | tradeoff         |
| Tags     | youtube, validation, refactor, dry, edit-metadata, uploader, parity-test, issue-10 |

## Context

Both the upload (`normalize`) and the new edit path must enforce the same shape
rules: title length, numeric category id, and the 500-character tag budget. The
edit path needed those checks; the choice was whether to reuse the upload's rules
or restate them inline for edits.

## Options considered

### Option A: Duplicate the shape rules inline in the edit path, guarded by a parity test
- **Pros**: each path is self-contained; no shared surface to coordinate.
- **Cons**: two copies of the same rules is exactly where they drift — a message,
  a threshold, or a stage/category label changes in one and not the other.
  Divergence between the two validation paths was judged the single most likely
  future bug. A parity test only *detects* drift after it's written; it doesn't
  prevent it.

### Option B: Extract shared shape-validators and call them from both paths
- **Pros**: `validateTitle`, `validateCategoryNumeric`, `validateTagsBudget` become
  one source of truth; the two paths cannot drift because they run the same code.
  The extraction is behavior-preserving — messages, stage, and category are
  byte-identical and the existing upload tests are unchanged. A parity test is
  retained anyway as cheap redundancy.
- **Cons**: introduces a shared helper surface both paths depend on.

## Decision

**Option B.** Extracted `validateTitle`, `validateCategoryNumeric`, and
`validateTagsBudget` out of `normalize` and reused them from the edit path rather
than duplicating inline rules behind a parity test. The deciding factor: divergence
between the two validation paths was the most likely future bug, and extraction
makes that divergence structurally impossible. The refactor is
behavior-preserving — error messages, `stage`, and `category` are byte-identical
and the upload tests pass unchanged. A parity test is kept as cheap redundancy on
top of the shared code.

## Consequences

- Upload and edit validation cannot drift — same helpers, same errors.
- The extraction was proven safe by the unchanged upload test suite.
- Future shape rules (new limits, new fields) get added once and apply to both
  paths automatically.

## Related decisions

- [Tag validation counts characters, not bytes](2026-07-08-tag-validation-counts-characters-runes-not-bytes.md) — the `tagsBudget` helper this extraction reuses.

## Revisit trigger

If upload and edit ever need genuinely *different* shape rules (e.g. an edit-only
constraint), split the relevant validator rather than branching inside the shared
helper.
