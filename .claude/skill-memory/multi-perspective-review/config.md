# Multi-Perspective Review Config — yt-mcp

<!-- rune-generated: 2026-07-06 | git: efcaded | rune: 1.0 -->

## Reviewer Overrides

always_include:
  - Security & Trust Reviewer (OAuth tokens, refresh-token handling, SSRF surface in resolveSource)

always_exclude:
  - Accessibility Reviewer (no UI)
  - i18n / l10n Reviewer (no user-facing localized text)

## Project Context

domain: YouTube upload automation — Go MCP server over the YouTube Data API v3
primary_languages: Go
architecture: Single stdio MCP binary; internal packages config/store/uploader/mcptool; local JSON ledger + audit log, no DB
urgency_default: normal
debt_tolerance: normal

## Custom Triage Rules

- Any change touching `internal/uploader/source.go` (resolveSource/downloadToTemp) → always include Security & Trust Reviewer (SSRF, unbounded download).
- Any change touching OAuth/token code (`cmd/yt-authorize`, `uploader.service`) → always include Security & Trust Reviewer.
- Any change to `internal/store/` → treat as correctness-critical (double-publish guard); scrutinize durability + concurrency.
- Any change to `internal/mcptool/` tool input/output structs → treat as public API change.

## Reviewer Voice Tuning

- Test Coverage / Correctness: strict — new logic must arrive with table tests (project bar).
- Naming Guardian: enforce Go conventions (PascalCase exports, "verb noun: detail" error strings).
