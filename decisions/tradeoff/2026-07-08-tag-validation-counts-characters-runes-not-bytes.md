# Tag validation counts characters (runes), not bytes

| Field    | Value            |
|----------|------------------|
| Date     | 2026-07-08       |
| Status   | accepted         |
| Category | tradeoff         |
| Tags     | youtube, tags, validation, unicode, runes, client-guard, uploader, issue-11 |

## Context

Issue #11 hardened the `snippet.tags` 500-limit guard in
`internal/uploader/uploader.go`. The pre-existing guard summed **bytes** via
`len(t)` per tag, but the YouTube Data API limit — and our acceptance criteria
and docs — are expressed in **characters** ("500 characters"). For ASCII the two
are identical, but for multi-byte tags (emoji, CJK) the byte count overstates the
true character count, so the client guard could falsely reject a tag set the
server would happily accept.

A client-side guard exists to give a fast, honest error; it must never be
*stricter* than the server it fronts, or it becomes a source of false rejections
with no recourse.

## Options considered

### Option A: Re-document the limit as bytes; keep `len(t)`
- **Pros**: zero code change; the guard already matches its own (byte) framing.
- **Cons**: contradicts YouTube's documented "500 characters" and our AC/docs;
  keeps false-rejecting valid emoji/CJK tag sets that the server would accept;
  bakes an encoding detail into a user-facing limit.

### Option B: Count characters via `utf8.RuneCountInString(t)`
- **Pros**: matches the documented "500 characters" and the server's actual
  behavior; only ever *widens* acceptance (a client guard should never be
  stricter than the server); ASCII is byte-for-byte identical so no regression
  for existing tag sets; worst case of a too-lenient guard is a deferred, honest
  server-side rejection at `videos.insert` — never silent corruption or false
  rejection of valid tags.
- **Cons**: the guard is now slightly optimistic — a pathological tag set right at
  the boundary could pass the client and still be rejected server-side.

## Decision

**Option B.** Switched the per-tag term to `utf8.RuneCountInString(t)` via a new
pure helper `tagsBudget`, keeping the `+2` spaced-tag auto-quote term, the `(n-1)`
comma term, and the `>500` threshold unchanged. The guiding invariant: a
client-side guard must never be stricter than the server. Rune counting matches
the documented character limit; the only failure mode it introduces (a boundary
tag set deferred to an honest server rejection) is strictly preferable to
false-rejecting valid multi-byte tags. This only widens acceptance; ASCII input
is unaffected.

## Consequences

- Valid emoji/CJK tag sets near the limit are no longer false-rejected client-side.
- The guard is deliberately non-authoritative at the exact boundary; the server
  remains the source of truth, surfacing a structured `invalid_request` if a
  too-lenient client guard lets an over-limit set through.
- Shipped with the **first-ever `internal/uploader` table test** (characterization
  of the existing byte/spacing/comma behavior + a rune-axis group proving
  multi-byte tags are counted by character, not byte).

## Related decisions

- [Tags are not a Shorts discovery lever](2026-07-08-tags-not-a-shorts-discovery-lever.md) — the parent research decision (#9) that scoped #11; this is its implementation-level follow-through on the tag-validation slice.

## Experiments

<!-- none — covered by the new internal/uploader table test rather than a benchmark -->

## Revisit trigger

If YouTube changes the documented tag limit unit (e.g. to bytes or a per-tag cap)
or the server begins rejecting rune-valid tag sets the client passes.
