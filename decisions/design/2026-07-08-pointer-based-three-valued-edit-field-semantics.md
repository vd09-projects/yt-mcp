# Pointer-based three-valued field semantics for edit requests

| Field    | Value            |
|----------|------------------|
| Date     | 2026-07-08       |
| Status   | accepted         |
| Category | design           |
| Tags     | youtube, videos-update, edit-metadata, pointers, forcesendfields, three-valued, uploader, issue-10 |

## Context

`edit_video_metadata` (#10) must distinguish three caller intents per field:
**preserve** (leave the current value untouched), **overwrite** (set a new value),
and **clear** (delete the value, e.g. empty the tag list or description). A plain
`string` / `[]string` collapses "preserve" and "clear" — both look like the zero
value. Worse, Google's generated YouTube client omits empty fields on the wire, so
an intentional clear silently vanishes.

## Options considered

### Option A: Plain value fields
- **Pros**: simplest struct.
- **Cons**: cannot distinguish preserve from clear; the generated client drops
  empties, so a clear never reaches the server.

### Option B: Pointer fields with three-valued encoding + ForceSendFields
- **Pros**: `*string` / `*[]string` with `omitempty` encode nil = preserve,
  non-nil non-empty = overwrite, non-nil empty = clear. Intentional clears are
  forced onto the wire by adding the field to `snippet.ForceSendFields` — the
  same force-send technique the insert path already uses for the `*bool`
  `SelfDeclaredMadeForKids` (an established, invariant-backed pattern in this
  codebase). Non-clearable fields (`title`, `categoryId`) are rejected before any
  API call.
- **Cons**: pointer plumbing is more verbose; callers/serializers must respect the
  nil-vs-empty distinction.

## Decision

**Option B.** The edit request uses `*string` / `*[]string` with `omitempty` to
encode the three intents: **nil = preserve**, **non-nil non-empty = overwrite**,
**non-nil empty = clear**. Intentional clears are made to survive the Google
generated client by adding the field to `snippet.ForceSendFields`, reusing the
exact force-send technique the insert path relies on for `*bool
SelfDeclaredMadeForKids` (so an explicit zero/empty isn't dropped as a Go zero
value). `title` and `categoryId` are **non-clearable** and are rejected before any
API call — YouTube requires them, so clearing them is a client-side error, not a
deferred server rejection.

## Consequences

- Callers get precise, unambiguous edit semantics; "clear this field" actually
  clears server-side instead of being silently dropped.
- ForceSendFields is now load-bearing for edits as well as inserts — anything that
  serializes the snippet must preserve it (see the videoService wire-test
  decision).
- `title`/`categoryId` clears fail fast with a structured `invalid_request`
  before spending an API call.

## Related decisions

- [videoService interface asserts ForceSendFields on the wire](2026-07-08-videoservice-interface-forcesendfields-on-wire.md) — the test that guarantees the force-send list actually reaches serialization.
- [snippet.localized set to nil for server reconciliation](2026-07-08-snippet-localized-nil-server-reconcile.md) — companion merge-snippet behavior.

## Revisit trigger

If YouTube adds newly-clearable snippet fields, or makes `title`/`categoryId`
optional, revisit the non-clearable set and the force-send list.
