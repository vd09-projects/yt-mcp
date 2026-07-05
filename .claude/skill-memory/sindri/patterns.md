# Learned Patterns — yt-mcp

<!-- rune-generated: 2026-07-06 | git: efcaded | rune: 1.0 -->

## Known Hot Spots

- `internal/uploader/uploader.go` — idempotency + partial-failure rollback correctness — every change needs a test covering dedup, in-flight lock, and rollback-on-partial-failure paths.
- `internal/store/store.go` — durability of the double-publish guard — atomic ledger write (tmp+rename); one process mutex serializes all file I/O. Watch: ledger loss = double-publish risk.
- `internal/uploader/source.go` — SSRF surface — `resolveSource`/`downloadToTemp` fetch arbitrary http(s) URLs with no host allowlist and no size cap.
- `cmd/yt-authorize/main.go` — OAuth 3-legged consent flow — state-param CSRF check, loopback redirect; changes here touch token minting.
- `internal/mcptool/tools.go` — public MCP tool surface — input/output struct changes are breaking for callers.

## Recurring False Positives

- `httpClient` in `source.go` has no wall-clock timeout — **intentional and documented** (large video downloads must not hit a fixed cap; caller context governs lifetime). Do not flag.
- `SelfDeclaredMadeForKids` forced via `ForceSendFields` — intentional so explicit `false` isn't dropped as a Go zero value (COPPA). Do not flag.
- Single global mutex over file I/O in `store` — intentional; uploads are slow/rare, contention is a non-issue at 2–3 channels.

## Established Conventions (Not in CLAUDE.md)

- Error message format: "verb noun: detail" (e.g., "open local file %s: %w").
- Pipeline errors are `*StageError{Stage, Category, Hint, Err}`; build them via `stageErr` / `invalid` helpers, not raw `errors.New`.
- Spec traceability: doc comments cite the problem-statement section (e.g., "spec §5.3"). Keep that when editing near those seams.

## Accepted Debt

- No tests anywhere — whole repo — follow-up: backfill table tests for `normalize`, idempotency, and rollback first; require tests on all new logic going forward.
- Ledger `persistLocked` does not `fsync` the temp file before rename — `internal/store/store.go` — follow-up: add `tmp.Sync()` before close/rename (it's the double-publish guard).
- No SSRF host allowlist / no download size cap — `internal/uploader/source.go` — follow-up: block private/link-local ranges and wrap body in `io.LimitReader`.
