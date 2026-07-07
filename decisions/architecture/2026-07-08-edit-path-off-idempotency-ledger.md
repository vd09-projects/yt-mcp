# Edit path is kept off the idempotency ledger and in-flight lock

| Field    | Value            |
|----------|------------------|
| Date     | 2026-07-08       |
| Status   | accepted         |
| Category | architecture     |
| Tags     | youtube, videos-update, idempotency, ledger, edit-metadata, uploader, issue-10 |

## Context

Issue #10 added the `edit_video_metadata` tool (`videos.update`) to
`yt-upload-mcp`. The upload/create path is guarded by the idempotency ledger and
an in-flight lock (a core invariant: "the ledger is the double-publish guard").
The question was whether the new edit path should participate in that same ledger
and lock.

## Options considered

### Option A: Route edits through the same idempotency ledger + in-flight lock
- **Pros**: superficial consistency — one code path wraps every mutating call.
- **Cons**: an edit targets a **caller-supplied `video_id`** and mutates an
  existing resource; it cannot mint a duplicate video, so there is no
  double-publish invariant to protect. Worse, injecting edit records into the
  create-dedup ledger would **corrupt** it: a later upload with an overlapping
  derived key could misread an edit record as a prior successful upload and skip
  a real publish (or return a stale result).

### Option B: Keep the edit path entirely off the ledger and lock
- **Pros**: preserves the ledger's single meaning (create-dedup / double-publish
  guard); no risk of an edit record poisoning upload deduplication; matches the
  actual threat model — edits are inherently non-duplicating.
- **Cons**: edits gain no replay/dedup protection (acceptable — re-issuing the
  same edit is idempotent at the API level and harmless).

## Decision

**Option B.** `EditMetadata` in `internal/uploader/edit.go` deliberately does not
touch the idempotency ledger or the in-flight lock. The ledger exists solely to
prevent duplicate *creates*; an edit mutates an existing, caller-named resource
and cannot create a duplicate, so there is no invariant for the ledger to
protect. Mixing edit records into the create-dedup store would actively corrupt
it — a subsequent upload could misinterpret an edit record as a prior upload
success. The ledger keeps exactly one meaning.

## Consequences

- The "ledger is the double-publish guard" invariant stays clean and
  single-purpose; nothing in the edit path can poison upload dedup.
- Edits have no replay/dedup layer, which is intentional and safe: re-applying
  the same metadata edit is naturally idempotent server-side.
- See the related decision on edits being unaudited in v1 — together these mean
  the edit path writes to neither the ledger nor the audit log.

## Related decisions

- [Edits are unaudited in v1](../tradeoff/2026-07-08-edits-unaudited-in-v1.md) — the companion scoping call; edit writes to neither ledger nor audit log.
- [Tags are not a Shorts discovery lever](../tradeoff/2026-07-08-tags-not-a-shorts-discovery-lever.md) — research #9 that scoped the edit tool (#10) as a correction utility.

## Revisit trigger

If the edit path ever needs replay protection (e.g. batched/automated edits where
an accidental double-apply is not harmless), design a **separate** edit-side
ledger rather than reusing the create-dedup one.
