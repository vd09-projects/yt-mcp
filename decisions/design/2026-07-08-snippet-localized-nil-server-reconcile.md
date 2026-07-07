# snippet.localized set to nil so the server reconciles it

| Field    | Value            |
|----------|------------------|
| Date     | 2026-07-08       |
| Status   | accepted         |
| Category | design           |
| Tags     | youtube, videos-update, localized, merge-snippet, edit-metadata, uploader, issue-10 |

## Context

`videos.update` round-trips a `snippet` that can carry a `localized` block
(localized title/description). When editing, the base video fetched for the merge
may contain a `localized` block that is stale relative to the new
title/description/defaultLanguage being written. Sending it back risks persisting
outdated localized text.

## Options considered

### Option A: Round-trip the fetched `localized` block on the merged snippet
- **Pros**: preserves whatever localized values already existed.
- **Cons**: the fetched block may be stale relative to the edit; writing it back
  can persist outdated localized title/description that no longer matches the base
  fields.

### Option B: Set `snippet.localized` to nil on the merged snippet
- **Pros**: lets the server reconcile `localized` from the authoritative base
  `title` / `description` / `defaultLanguage` instead of trusting a possibly-stale
  client-held block.
- **Cons**: relies on server-side reconciliation behavior rather than explicit
  client control of localized values.

## Decision

**Option B.** `mergeSnippet` sets `snippet.localized` to nil on the outgoing
merged snippet, so the server reconciles the localized block from the base
title/description/defaultLanguage rather than round-tripping a possibly-stale
`localized` object the client happened to fetch. The base fields are the source of
truth; the server derives localized from them.

## Consequences

- No risk of persisting a stale localized block during an edit.
- Localized values are server-derived, not client-controlled — acceptable for the
  v1 correction utility, which does not manage per-locale text.

## Related decisions

- [Pointer-based three-valued field semantics](2026-07-08-pointer-based-three-valued-edit-field-semantics.md) — same mergeSnippet construction.

## Revisit trigger

If the tool ever needs to set or preserve explicit per-locale
titles/descriptions, stop nil-ing `localized` and manage it as a first-class field.
