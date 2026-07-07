# Last-writer-wins accepted; no ETag/If-Match optimistic concurrency in v1

| Field    | Value            |
|----------|------------------|
| Date     | 2026-07-08       |
| Status   | accepted         |
| Category | tradeoff         |
| Tags     | youtube, videos-update, concurrency, etag, lost-update, edit-metadata, uploader, issue-10 |

## Context

`edit_video_metadata` does a fetch-merge-update against a live YouTube video. In
theory, a concurrent edit (another operator, or the YouTube UI) between the fetch
and the update could be silently overwritten — a classic lost-update. ETag /
If-Match optimistic concurrency would detect this and reject the stale write.

## Options considered

### Option A: Add ETag / If-Match optimistic concurrency
- **Pros**: detects and rejects lost-update collisions.
- **Cons**: added complexity and a retry/reconcile story for a tool that is a
  single-operator correction utility, where concurrent edits to the same video are
  not an expected mode.

### Option B: Accept last-writer-wins for v1
- **Pros**: matches the actual usage — one operator correcting their own video's
  metadata; no concurrency-control machinery to build or reason about.
- **Cons**: a concurrent edit between fetch and update is silently overwritten
  (lost update). Acceptable given the single-operator model.

## Decision

**Option B.** Last-writer-wins / lost-update is an **accepted** characteristic of
this single-operator correction utility. v1 ships no ETag/If-Match optimistic
concurrency. The tool corrects a video's own operator's metadata; concurrent
edits to the same resource are outside the expected usage, so the cost of
optimistic concurrency isn't justified yet.

## Consequences

- A concurrent edit (second operator, or the YouTube Studio UI) between fetch and
  update can be silently clobbered.
- No collision detection or retry story exists — by design, for now.

## Related decisions

- [Edit path is kept off the idempotency ledger](../architecture/2026-07-08-edit-path-off-idempotency-ledger.md) — same "edits are single-operator, non-duplicating" framing.

## Revisit trigger

If edits become multi-operator or automated (batch/scheduled edits), add
ETag/If-Match optimistic concurrency so stale writes are rejected rather than
silently overwriting a concurrent change.
