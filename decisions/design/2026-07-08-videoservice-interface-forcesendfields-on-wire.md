# videoService interface asserts ForceSendFields reaches the wire

| Field    | Value            |
|----------|------------------|
| Date     | 2026-07-08       |
| Status   | accepted         |
| Category | design           |
| Tags     | youtube, videos-update, testing, forcesendfields, seam, fake, edit-metadata, uploader, issue-10 |

## Context

The edit path's correctness depends on `snippet.ForceSendFields` (and, by
extension, `NullFields`) actually being present when the request is serialized —
that's how an intentional field clear survives Google's generated client. Testing
`mergeSnippet`'s return value proves the merge is correct *in memory*, but not that
the force-send list survives all the way to the wire. There was no seam to observe
the outgoing request.

## Options considered

### Option A: Assert only at the mergeSnippet return boundary
- **Pros**: no new interface; unit-test the pure merge function directly.
- **Cons**: leaves a whole class of bug uncovered — a merge that is correct in
  memory but whose ForceSendFields is dropped before serialization would pass. The
  exact failure mode the pointer/force-send design is guarding against would go
  undetected.

### Option B: Introduce an unexported videoService interface (List + Update) and a fake
- **Pros**: the fake captures the actual `*youtube.Video` handed to `Update`, so
  the test asserts `ForceSendFields` **on the wire**, not just at the merge return.
  Closes the "correct-in-memory, dropped-before-serialization" gap. Keeps the seam
  package-local and unexported.
- **Cons**: one more internal interface + fake to maintain.

## Decision

**Option B.** Introduced an unexported package-local `videoService` interface
(`List` + `Update`) so a fake can capture the outgoing snippet and assert
`ForceSendFields` at the serialization boundary — not merely at `mergeSnippet`'s
return. This closes the class of bug where the merge is correct in memory but the
force-send list is dropped before the request is serialized. Broader takeaway
recorded for the codebase: **any `videos.update` / `*.update`-with-`part=` write
should test that `ForceSendFields`/`NullFields` actually reach the wire**, because
the generated client's omit-empty behavior makes in-memory correctness
insufficient.

## Consequences

- The edit path has a regression guard on its most fragile property (force-send
  survival), not just on merge logic.
- A reusable testing seam exists for future mutating YouTube calls.
- Establishes a codebase convention: wire-level assertion for any partial-update
  write that relies on ForceSendFields/NullFields.

## Related decisions

- [Pointer-based three-valued field semantics](2026-07-08-pointer-based-three-valued-edit-field-semantics.md) — the design whose ForceSendFields this test protects.

## Revisit trigger

When a second mutating YouTube call is added, promote this seam/convention into a
shared test helper rather than re-deriving it per package.
