# Tags are not a Shorts discovery lever

| Field    | Value            |
|----------|------------------|
| Date     | 2026-07-08       |
| Status   | accepted         |
| Category | tradeoff         |
| Tags     | youtube, shorts, tags, discovery, metadata, videos-update, research-9 |

## Context

An operator uploaded a Short via `yt-upload-mcp`'s `upload_video` with no tags; it sat at ~0 views. After manually adding 7 tags in the YouTube mobile app, the video reached ~30 views / 2 likes in ~5h. They concluded the tags caused the push and asked to set tags via the API. This triggered research **#9** to decide where to invest for Shorts discovery and what, if anything, to build.

## Options considered

### Option A: Treat tags as a discovery lever (build tag-focused features)
- **Pros**: matches the operator's intuition; tags are easy to set.
- **Cons**: not supported by evidence. Official YouTube docs call tags *"Not important"* (misspellings only); the Shorts feed ranks on engagement signals with tags absent; YouTube attributes post-edit view shifts to viewer interaction *"rather than the act of changing"* metadata. The operator's jump is a confounded correlation (no re-index support; normal feed ramp; noisy count — a Short view = every play, no min watch time since 2025-03-31).

### Option B: Keep the existing tags-at-upload path; invest in engagement + title/description/hashtags
- **Pros**: matches what the feed actually ranks on; tags-at-upload already works today (`tags` field → `snippet.tags`, `tools.go:28`) at zero cost; honest about causation.
- **Cons**: doesn't give the operator the "one knob boosts views" they hoped for.

## Decision

**Option B.** Verified 7/7 load-bearing claims against authoritative YouTube docs (6 verified + 1 verified-by-corroboration; 0 contested). Tags do **not** meaningfully drive Shorts views. Keep the existing tags-at-upload mechanism (already complete), do **not** treat tags as a discovery lever, and steer optimization toward engagement/retention + title/description/topical hashtags. Add a `videos.update` edit-metadata tool only as a **correction utility**, explicitly not a views booster.

## Consequences

- Docs must correct the operator's mental model and the legacy quota figure (`videos.insert` = 1 unit / 100-per-day bucket, not ~1600).
- New feature work is scoped as correction/validation, not growth hacking: #10 (`videos.update` editor), #11 (500-char tag validation), #12 (docs), #13 (optional discovery helper).
- Future "why aren't tags helping?" questions are pre-answered by the report.

## Related decisions

<!-- none yet — first decision in this journal -->

## Experiments

Research #9, adversarial verification (huginn): 4 parallel scouts → opinion → 7 blind claim-verifiers (C1–C7), refutation-first. All 7 survived. Full report + sources: `research/tags-shorts-discovery-metadata-api/report.md`.

## Revisit trigger

If YouTube publishes Shorts-specific ranking documentation that lists metadata/tags as a feed signal, or if a controlled A/B (same content, tags vs no-tags) shows a reproducible view delta.
