# Tags do not meaningfully drive Shorts views

> Research #9 — *do tags drive Shorts views, and how to set/edit video metadata via the YouTube Data API v3.*
> Status: **complete** — 7/7 load-bearing claims verified (6 verified + 1 verified-by-corroboration; 1 sub-claim inferred; 0 contested/unsupported).

## Trigger

An operator uploaded a Short via `yt-upload-mcp`'s `upload_video` with **no tags**; it sat at ~0 views. After manually adding 7 tags in the YouTube mobile app (`redit`, `shortstories`, `reditstories`, `sadstories`, `heartfelt`, `cutestories`, `reddit`), the video reached ~30 views / 2 likes in ~5h. They believed the tags caused the push and wanted to do it via API.

## Recommendation

1. **Setting tags at upload already works today** — our `tags []string` → `snippet.tags` on `videos.insert` (`internal/mcptool/tools.go:28` → `internal/uploader/uploader.go:179,193`) is the complete, correct mechanism. The failed upload simply passed no tags. Add client-side validation of the **500-char aggregate budget** (commas + auto-quotes on spaced tags count: `Foo-Baz`=7, `Foo Baz`=9).
2. **Do not expect tags to move Shorts views.** The operator's 0→~30 views/5h is a **confounded correlation, not causation.** Official docs call tags *"Not important"* / *"minimal role"* (misspellings), list only engagement signals as recommendation drivers, and explicitly attribute post-edit performance shifts to changed viewer interaction *"rather than the act of changing"* metadata. The believed edit→re-index→boost mechanism has **zero authoritative support**.
3. **Invest in what the Shorts feed ranks on:** engagement/retention (hook, watch-through, likes), then a strong title + description + a few relevant topical hashtags (help Shorts *search* + sounds pages). All settable via `videos.insert`.
4. **Add a `videos.update` "edit metadata" tool** as a follow-up — cheap (50u) and useful for correcting metadata post-publish, built list→mutate→update (title+categoryId required, omitted fields clobbered). A correction utility, **not** a views booster.

## Findings by acceptance criterion

### AC1 — set tags at upload
`snippet.tags` is a list; **500-char aggregate max**; `part=snippet` required; tags optional (empty tag → 400). Our field is complete. `[C3 verified]`

### AC2 — edit tags after upload
`videos.update` **deletes omitted writable fields**; `title` + `categoryId` are **required** on a snippet update; correct pattern = `videos.list part=snippet` → mutate → `videos.update`. Quota: update = 50u, list = 1u, insert = 1u (in a separate 100-uploads/day bucket). Worth building as a follow-up. `[C4, C5 verified]`

### AC3 — do tags drive Shorts views? (LOAD-BEARING)
**No.** Tags "not important"; the feed ranks on % who chose to view / avg view duration / avg % viewed / likes / surveys; metadata is cited only search-side. Confounders for the operator's jump:
- (a) edit→reindex/boost — **no support, contradicted** `[C6]`;
- (b) normal Shorts feed ramp — model-only, no official quote;
- (c) low-count noise — a Short "view" counts every play with no minimum watch time (effective 2025-03-31).

`[C1, C2, C6 verified]`

### AC4 — what drives discovery + API surface
Engagement primary; title/description > tags; hashtags 3-shown / >60-ignored (no official "15" limit); `categoryId` has no documented discovery effect; made-for-kids disables discovery surfaces. Settable on `videos.insert`: `title`, `description` (hashtags live as text inside these — no dedicated field), `categoryId`, `tags`, `defaultLanguage`, `status.selfDeclaredMadeForKids`. `status.madeForKids` is read-only. Already exposed by our tool: all but `defaultLanguage` + a topical-hashtag/description helper. `[C7, SQ4 verified]`

## Load-bearing claims & grades

| Claim | Grade |
|---|---|
| C1 — tags "minimal role" / "not important" (misspellings) | **verified** (by corroboration — answer/141805 quoted verbatim by the C6 and C2 verifiers independently + answer/146402 via SQ4; dedicated C1 verifier did not complete) |
| C2 — Shorts recommendations rank on engagement signals; tags not listed | **verified** (core); "metadata affects search only, never the feed" = **inferred** (documented silence, not a verbatim denial) |
| C3 — `snippet.tags` 500-char aggregate; commas + auto-quotes count | **verified** |
| C4 — `videos.update` clobbers omitted fields; title+categoryId required on snippet update | **verified** |
| C5 — insert=1u (100/day bucket), update=50u, list=1u; ~1600 is legacy (changed 2025-12-04) | **verified** |
| C6 — no authoritative source that editing metadata re-indexes/boosts; YouTube attributes post-edit shifts to viewer interaction "rather than the act of changing" metadata | **verified** |
| C7 — hashtags: 3 shown, >60 ignored, no official "15" | **verified** |

## Contradictions surfaced (not averaged)

- **Insert quota cost:** current official docs = 1u (100/day bucket) vs the widely-repeated legacy ~1600u → **current wins** (changed 2025-12-04).
- **Hashtag count:** blogs cite "15 hashtag limit" vs official "3 shown / >60 ignored" → **official wins**.
- **Tags help views:** SEO/marketing blogs vs official "not important" → **official wins** (blogs cite no authority).

## Open unknowns

- Whether metadata *ever* touches the Shorts feed (vs search only) — inferred, not stated.
- Exact early-ramp curve — no official quote.
- Whether API metadata edits can change a video's Shorts classification — undocumented.

## Example call — tags at upload

```json
{
  "channel": "shorts",
  "source": "https://.../short.mp4",
  "title": "She waited 20 years for this letter #Shorts",
  "description": "A heartfelt reunion. #reddit #shortstories #sadstories",
  "is_short": true,
  "self_declared_made_for_kids": false,
  "tags": ["reddit", "shortstories", "reddit stories", "sad stories", "heartfelt"]
}
```

## Sources

- developers.google.com/youtube/v3/docs/videos, /videos/insert, /videos/update, /determine_quota_cost
- support.google.com/youtube/answer/146402, /141805, /16559651, /16089387, /11914225, /6390658, /9527654
