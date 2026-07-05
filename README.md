# yt-mcp

A monorepo of Go MCP (Model Context Protocol) servers for YouTube automation.
The first (and currently only) server, **`yt-upload-mcp`**, publishes a video
or Short to one of a fixed set of pre-configured YouTube channels via the
official YouTube Data API v3 — no manual YouTube Studio work.

Implements the v1 scope of the problem statement: single-video upload with
full metadata, local-path and remote-URL sources, idempotency against
duplicate publishes, rollback on partial failure, and a structured audit
trail. Analytics, playlist curation beyond "add to playlist", comments and
live streaming are explicitly out of scope (deferred to Phase 2 — planned as
a **separate server binary** in this repo, see the Phase 2 section).

## Architecture

```
cmd/
  yt-upload-mcp/     MCP stdio server entry point (the v1 upload tool)
  yt-authorize/      one-time OAuth consent CLI (run once per channel;
                     --scopes flag supports minting Phase 2 tokens)
internal/
  config/            static channel-routing config (alias -> credentials + defaults)
  store/             idempotency ledger (idempotency.json) + audit trail (audit.log)
  uploader/          the pipeline: validate -> idempotency -> guards ->
                     videos.insert -> thumbnails.set -> playlistItems.insert,
                     with rollback + error categorization
  mcptool/           MCP tool surface: upload_video, list_channels, verify_channels
```

One repo, shared internals (`config`, `store`, error taxonomy), but **one
server binary per capability domain**. A future analytics server reuses the
channel registry and audit trail while keeping its own read-only OAuth tokens
and its own MCP registration — so an agent given analytics access cannot
publish, enforced by Google-side scopes rather than prompt discipline.

Design decisions, mapped to the problem statement:

| Spec | How it's implemented |
|---|---|
| §3 caller decides, tool executes | The tool has no channel-selection logic. `channel` is a required input; aliases come from static config. |
| §4.1 static routing | `config.json` maps alias → refresh token + defaults. Adding a channel = config edit + one `yt-authorize` run. |
| §4.2 never silently public | Privacy default chain: caller → channel default → `unlisted`. Scheduled publishes force/require `private`. |
| §4.3 Shorts are declared, not inferred | Explicit `is_short` flag; the tool appends `#Shorts` to the description if missing. Deterministic. |
| §5.1 one refresh token per channel | Shared OAuth client, per-channel token in config. `invalid_grant` failures carry a hint about the 7-day Testing-mode expiry. |
| §5.3 platform risk guards | Cross-channel duplicate detection via content SHA-256 (warn or block, configurable) and an optional minimum-spacing guard across channels. |
| §6 idempotency | Required key (caller-supplied or derived as `sha256:<hash>@<channel>`). A prior success returns the existing result. In-flight keys are locked against concurrent retries. |
| §7 failure handling | Every failure is a structured `{stage, category, hint}`. Partial failures after upload roll back (delete the video) by default; a failed rollback surfaces the orphaned video ID explicitly. |
| §8 audit trail | `state/audit.log` (JSONL): channel, timestamp, outcome, stage+category on failure, video ID/URL on success, duration. |

## Prerequisites

- Go 1.23+
- A Google Cloud project with the **YouTube Data API v3** enabled
- Manager access to each target YouTube channel

## Setup

### 1. Google Cloud project (once)

1. Create a project at https://console.cloud.google.com
2. **APIs & Services → Library** → enable *YouTube Data API v3*
3. **APIs & Services → OAuth consent screen**:
   - User type: External
   - Add the Google accounts that manage your channels as **Test users**
   - Publishing status starts as **Testing** — see the credential-lifecycle
     note below
4. **APIs & Services → Credentials → Create credentials → OAuth client ID**:
   - Application type: **Desktop app** (this is what allows the loopback
     redirect that `yt-authorize` uses)
   - Note the client ID and client secret

> **Credential lifecycle (spec §5.1).** While the consent screen is in
> *Testing* status, refresh tokens expire every **7 days** and `yt-authorize`
> must be re-run per channel. The target state is Google app verification
> (*In production*) for long-lived tokens; note that the `youtube.upload` and
> `youtube.force-ssl` scopes are sensitive scopes, so verification involves a
> review. Until then, run the `verify_channels` tool before a batch of
> uploads to catch expired tokens early — an expired token fails with
> `auth_error` and an `invalid_grant` hint.

### 2. Build

```bash
go mod tidy   # resolves and pins dependency versions
make build    # produces bin/yt-upload-mcp and bin/yt-authorize
```

### 3. Authorize each channel (once per channel, or every 7 days in Testing mode)

```bash
export YT_CLIENT_ID="...apps.googleusercontent.com"
export YT_CLIENT_SECRET="..."

./bin/yt-authorize --client-id "$YT_CLIENT_ID" --client-secret "$YT_CLIENT_SECRET"
```

Open the printed URL, sign in with the account that manages the target
channel (pick the right brand account if prompted), and approve. The CLI
prints **which channel the token controls** plus the refresh token. Export
it, e.g. `YT_REFRESH_TOKEN_MAIN`. Repeat per channel.

### 4. Configure

```bash
cp config.example.json config.json
```

Secrets stay in the environment; the config references them with `${VAR}`:

```json
{
  "oauth": { "client_id": "${YT_CLIENT_ID}", "client_secret": "${YT_CLIENT_SECRET}" },
  "state_dir": "state",
  "block_cross_channel_duplicates": true,
  "min_seconds_between_uploads": 0,
  "channels": {
    "main":   { "refresh_token": "${YT_REFRESH_TOKEN_MAIN}",   "default_category_id": "28", "default_privacy": "unlisted" },
    "shorts": { "refresh_token": "${YT_REFRESH_TOKEN_SHORTS}", "default_category_id": "24", "default_privacy": "unlisted" }
  }
}
```

Config knobs:

- `block_cross_channel_duplicates` — `true`: reject content whose SHA-256 was
  already published to a *different* channel unless the call passes
  `allow_cross_channel_duplicate`; `false`: proceed with a warning (spec §5.3).
- `min_seconds_between_uploads` — `> 0` rejects uploads fired closer together
  than this across all channels (timing-clustering guard, spec §5.3); the
  error includes a retry-after hint. `0` disables.
- `upload_chunk_size_mb` — resumable-upload chunk size (default 8).

### 5. Register with an MCP client

Claude Code:

```bash
claude mcp add --transport stdio yt-upload \
  --env YT_CLIENT_ID="$YT_CLIENT_ID" \
  --env YT_CLIENT_SECRET="$YT_CLIENT_SECRET" \
  --env YT_REFRESH_TOKEN_MAIN="$YT_REFRESH_TOKEN_MAIN" \
  --env YT_REFRESH_TOKEN_SHORTS="$YT_REFRESH_TOKEN_SHORTS" \
  -- /absolute/path/to/bin/yt-upload-mcp --config /absolute/path/to/config.json
```

Claude Desktop (`claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "yt-upload": {
      "command": "/absolute/path/to/bin/yt-upload-mcp",
      "args": ["--config", "/absolute/path/to/config.json"],
      "env": {
        "YT_CLIENT_ID": "...",
        "YT_CLIENT_SECRET": "...",
        "YT_REFRESH_TOKEN_MAIN": "...",
        "YT_REFRESH_TOKEN_SHORTS": "..."
      }
    }
  }
}
```

The server logs to stderr only; stdout is reserved for the MCP protocol.

## Tools

### `upload_video`

| Field | Required | Notes |
|---|---|---|
| `channel` | yes | configured alias; see `list_channels` |
| `source` | yes | local absolute path **or** http(s) URL (e.g. GitHub-hosted) |
| `title` | yes | ≤ 100 chars, no `<` `>` |
| `description` | yes | ≤ 5000 bytes; hashtags can live here |
| `tags` | no | backend keywords, distinct from hashtags; ~500-char combined limit |
| `category_id` | yes* | numeric; *falls back to the channel default |
| `privacy_status` | no | `public` / `unlisted` / `private`; defaults to `unlisted`, never silently public |
| `self_declared_made_for_kids` | yes | explicit `true`/`false` (COPPA) |
| `thumbnail` | no | path or URL, ≤ 2 MB; needs a phone-verified channel |
| `playlist_id` | no | added after upload |
| `publish_at` | no | RFC3339, future; forces/requires `private` |
| `is_short` | no | explicit flag; tool appends `#Shorts`, never infers |
| `idempotency_key` | no | derived as `sha256:<hash>@<channel>` if omitted; **reuse on retries** |
| `rollback_on_partial_failure` | no | default `true` |
| `allow_cross_channel_duplicate` | no | overrides the §5.3 block |

Success response:

```json
{
  "status": "success",
  "channel": "main",
  "video_id": "dQw4w9WgXcQ",
  "video_url": "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
  "idempotency_key": "run-2026-07-06-ep12",
  "privacy_status": "unlisted",
  "thumbnail_set": true,
  "warnings": []
}
```

Failure response (structured per spec §7 — exact stage, categorized reason):

```json
{
  "status": "error",
  "stage": "set_thumbnail",
  "category": "auth_error",
  "error": "googleapi: Error 403: ... (note: custom thumbnails require a phone-verified channel, spec §4.2)",
  "rolled_back": true
}
```

Categories: `auth_error`, `quota_exceeded`, `invalid_request`,
`network_error`, `policy_violation` (local §5.3/§6 guards), `other`.
Pipeline stages: `validate`, `idempotency_check`, `resolve_source`,
`platform_guards`, `insert_video`, `set_thumbnail`, `add_to_playlist`.

### `list_channels`

Returns the configured aliases and their defaults (never secrets), so the
calling application can discover where it may publish.

### `verify_channels`

1-quota-unit `channels.list` per channel. Confirms each refresh token still
works and reports the channel title/ID it controls. Run before batches while
in Testing mode (7-day token expiry).

## Idempotency and retries (spec §6)

- Every upload carries a key: caller-supplied, or derived from the content
  SHA-256 + channel.
- Before `videos.insert`, the key is checked against
  `state/idempotency.json`; a prior **success** returns the existing result
  with `"status": "deduplicated"` — nothing is re-published.
- A key already in flight (concurrent retry) is rejected with
  `policy_violation` rather than racing.
- Failed and rolled-back attempts do **not** block a retry with the same key.
- Timeout guidance: if a call times out, retry **with the same key**. If the
  slow attempt actually succeeded, the retry deduplicates instead of
  double-publishing.

## Failure handling and rollback (spec §7)

If `videos.insert` succeeds but a follow-up step (thumbnail, playlist)
fails:

- default (`rollback_on_partial_failure: true`): the video is deleted, the
  ledger records `rolled_back`, and the error names the failed stage. Note
  `videos.delete` costs 50 quota units.
- if the rollback delete itself fails: the error carries
  `orphaned_video_id` and both errors — delete manually before retrying.
- `rollback_on_partial_failure: false`: the video is kept, recorded as a
  success, and the failed step is downgraded to a warning.

## Logging (spec §8)

`state/audit.log` is append-only JSONL, one entry per attempt:

```json
{"timestamp":"2026-07-06T10:12:03Z","channel":"main","outcome":"failure","stage":"insert_video","category":"quota_exceeded","error":"...","idempotency_key":"run-12","duration_ms":8412}
```

`state/idempotency.json` is the ledger snapshot (atomic writes). Back up the
state dir if the history matters; deleting it resets idempotency.

## Quota (spec §5.2)

Per upload: `videos.insert` 100 units, `thumbnails.set` ~50, `playlistItems.insert` ~50,
`videos.delete` (rollback only) 50, `channels.list` (verify) 1. At 3–5
uploads/day across all channels against the shared 10,000/day project pool,
headroom is large; `quota_exceeded` failures carry a hint either way.

## Phase 2 (explicitly deferred)

Fan-channel analytics is planned as a **separate server binary**
(`cmd/yt-analytics-mcp/`) in this repo, not more tools on the upload server:

- **Least privilege.** Analytics tokens use only
  `https://www.googleapis.com/auth/yt-analytics.readonly` — an agent holding
  them cannot publish, enforced by Google, not by prompts.
- **Independent registration.** Attach only the server a project needs;
  upload-automation sessions don't pay context cost for analytics tool
  schemas, and vice versa.
- **Shared internals.** The new server reuses `internal/config` (channel
  registry) and `internal/store` (audit), with its own token fields.

To mint an analytics token when the time comes (re-consent per channel is
required for the new scope anyway):

```bash
./bin/yt-authorize --config config.json \
  --scopes https://www.googleapis.com/auth/yt-analytics.readonly
```

Prefer a separate token per capability over one token carrying every scope.
