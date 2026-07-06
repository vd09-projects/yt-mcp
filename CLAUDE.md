<!-- rune-generated: 2026-07-06 | git: efcaded | rune: 1.0 -->

# yt-mcp

A monorepo of Go MCP servers for YouTube automation; v1 is `yt-upload-mcp`, which
publishes a video or Short to a pre-configured channel via the YouTube Data API v3.

## Stack

- **Language:** Go 1.23+
- **Framework:** MCP (`github.com/modelcontextprotocol/go-sdk`), stdio transport
- **External API:** YouTube Data API v3 (`google.golang.org/api/youtube/v3`), OAuth2 (`golang.org/x/oauth2`)
- **Storage:** local JSON files — `idempotency.json` (atomic snapshot) + `audit.log` (JSONL). No database.
- **Deployment:** single static binary per capability domain, launched by an MCP client over stdio
- **Key libs:** google.golang.org/api, golang.org/x/oauth2, go-sdk/mcp

## Architecture

Two commands (`cmd/yt-upload-mcp` server, `cmd/yt-authorize` one-time OAuth CLI) over four
internal packages: `config` (static alias→credentials routing), `store` (idempotency ledger +
audit trail, one process mutex), `uploader` (the pipeline: validate → idempotency → platform
guards → videos.insert → thumbnails.set → playlistItems.insert, with rollback on partial
failure and a fixed error taxonomy), `mcptool` (three tools: upload_video, list_channels,
verify_channels). Hard boundary: the tool never decides *which* channel — the caller does
(spec §3); channel routing is setup-time config only.

## Invariants (never violate)

- **Never silently public.** Privacy default chain: caller → channel default → `unlisted`. Scheduled publishes force/require `private`.
- **Idempotent uploads.** Every upload has a key (caller-supplied or derived `sha256:<hash>@<channel>`). A prior success returns the existing result; in-flight keys are locked against concurrent double-publish.
- **COPPA declaration required.** `self_declared_made_for_kids` is a required `*bool`; force-sent so an explicit `false` isn't dropped as a Go zero value.
- **Shorts are declared, not inferred.** Explicit `is_short` flag; tool appends `#Shorts`, never guesses from aspect ratio.
- **Structured failures.** Every failure is `{stage, category, hint}` from the fixed taxonomy (auth_error, quota_exceeded, invalid_request, network_error, policy_violation, other). Partial failure after insert rolls back by default; a failed rollback surfaces the orphaned video ID explicitly — never a generic error.
- **The ledger is the double-publish guard.** If the video published but the ledger write failed, surface it loudly (CRITICAL warning), never pretend success.

## Gotchas

- **7-day token expiry.** While the OAuth consent screen is in *Testing*, refresh tokens die every 7 days. `invalid_grant` → re-run `yt-authorize --channel <alias> --config config.json` (rewrites that channel's `token_file`). Run `verify_channels` before an upload batch.
- **Tokens live in files, not env.** Each channel's `token_file` (config field, `${VAR}`-expandable path) points at a JSON `{ refresh_token, channel_id, obtained_at, scopes }` written by `yt-authorize` and read at `config.Load`. The env/config holds the *path*, never the secret (GOOGLE_APPLICATION_CREDENTIALS-style). The legacy `refresh_token` config field is rejected with a migration error.
- **Secrets never in git.** `config.json`, `state/`, `*.token.json`, `*.log` are gitignored; only `config.example.json` is tracked. Client credentials resolve from env via `${VAR}` expansion; channel tokens resolve from `token_file`.
- **stdout is the MCP protocol.** All diagnostics go to stderr; anything on stdout corrupts JSON-RPC.
- `resolveSource` fetches arbitrary http(s) URLs (SSRF surface) with no size cap — treat as security-sensitive.

## Quick conventions

- **Errors:** wrap with `%w`; pipeline errors are `*StageError` (stage + category + hint). Message style: "verb noun: detail".
- **Logging:** `log` to stderr only, prefix `yt-upload-mcp: `.
- **Config/secrets:** static JSON + `${ENV_VAR}` expansion; never commit real tokens.
- **Testing:** new or changed logic ships with Go table tests (stdlib `testing`). Backfill coverage for the existing pipeline over time. Security-sensitive changes (OAuth, tokens, `resolveSource`) always get a multi-perspective-review pass.
- **Scope:** v1 hardening only. Analytics/comments/live are deferred; do NOT bake Phase-2 assumptions into code yet.

## Team / process

- Default merge flow: **feature branch → PR → review-required** (multi-perspective-review gates). Lighter paths (solo PR self-review, or direct-to-main) allowed per task when the change is trivial; the *default* is the reviewed PR.
- Tasks tracked as **GitHub issues** on `vd09-projects/yt-mcp` (task-manager, backend: github). Task ID = issue number.

## Skills installed

- `sindri` — implementation (plan/build/iterate/spike)
- `mimir` — architecture & task planning
- `multi-perspective-review` — code review
- `skald` — handoff persistence layer
- `task-manager` — GitHub-issues backlog
- `huginn` (+ research-scout, claim-verifier) — deep tech research
- `web-pilot` — drive real browser for web console tasks (OAuth client, GitHub tokens)
- `decision-journal`, `conventional-commits`, `rune`, `setup-session`

## Re-run rune when

- Go version, MCP SDK, or storage model changes
- A core invariant is added/removed/disproven
- Phase 2 direction is decided (separate binaries per domain)
- Review/team requirements change significantly
