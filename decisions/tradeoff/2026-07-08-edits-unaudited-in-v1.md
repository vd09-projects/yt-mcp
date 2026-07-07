# Edits are unaudited in v1 (no changed_fields in the audit schema)

| Field    | Value            |
|----------|------------------|
| Date     | 2026-07-08       |
| Status   | accepted         |
| Category | tradeoff         |
| Tags     | youtube, audit, edit-metadata, uploader, deferred, todo, issue-10 |

## Context

Issue #10's `edit_video_metadata` mutates a video's snippet. Recording *which*
fields an edit changed would be the natural audit-trail entry. But `store.AuditEntry`
has no `changed_fields` member, and adding one is a storage-schema change the plan
put out of scope for v1.

## Options considered

### Option A: Extend `store.AuditEntry` with `changed_fields` now
- **Pros**: edits become fully audited on day one.
- **Cons**: a schema change to the audit log format, explicitly marked
  out-of-scope by the #10 plan; widens the blast radius of an otherwise contained
  feature; risks bikeshedding the field shape before there's a consumer.

### Option B: Ship edits unaudited in v1; mark a follow-up
- **Pros**: keeps #10 contained to the edit pipeline; no audit-schema churn;
  defers the schema shape until it can be aligned with a real consumer.
- **Cons**: v1 edits leave no audit trail; a `// TODO` and a tracking issue are
  the only record that this is intentional.

## Decision

**Option B.** `EditMetadata` writes **neither** the audit log **nor** the ledger.
Adding `AuditEntry.changed_fields` is a schema change marked out of scope by the
plan, so v1 edits are intentionally unaudited. A `// TODO` marker in
`internal/uploader/edit.go` plus a follow-up issue track adding the field later.
When it is added, align its names to the caller-facing field names that
`UpdatedFields` already emits, so the audit vocabulary and the tool-response
vocabulary match.

## Consequences

- v1 has no server-side record of what an edit changed — acceptable for a
  single-operator correction utility, but a known gap.
- The follow-up must reuse the `UpdatedFields` naming so audit and API responses
  don't diverge into two vocabularies for the same fields.
- Pairs with the decision to keep the edit path off the ledger: the edit pipeline
  writes to no persistent store in v1.

## Related decisions

- [Edit path is kept off the idempotency ledger](../architecture/2026-07-08-edit-path-off-idempotency-ledger.md) — the other half of "edits touch no persistent store in v1".

## Revisit trigger

When edits need traceability (multi-operator use, compliance, or debugging "who
changed this and when") — add `AuditEntry.changed_fields`, named to match
`UpdatedFields`.
