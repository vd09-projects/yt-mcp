# Sindri Config — yt-mcp

<!-- rune-generated: 2026-07-06 | git: efcaded | rune: 1.0 -->

## Language

primary_language: go
language_version: go1.23

## Scope

Whole codebase. `cmd/` and `internal/` are both in scope. Public surface is the three
MCP tools in `internal/mcptool` — signature/behavior changes there ripple to callers, so
treat tool input/output structs as an interface (needs planning, not a drive-by edit).

## Quality Overrides

- New or changed logic ships with Go table tests (stdlib `testing`). confidence: HIGH
- Security-sensitive paths — OAuth flow (`cmd/yt-authorize`, `uploader.service`), token
  handling, and `internal/uploader/source.go` (`resolveSource` / `downloadToTemp`) — always
  carry a multi-perspective-review pass regardless of change size.
- Backfill test coverage for the existing pipeline opportunistically when touching it
  (repo shipped v1 with 0 tests).

## Interrogation Defaults

- default_stage: build
- test_framework: go stdlib testing (table-driven)
- primary_command: `go build ./... && go vet ./... && go test ./...`

## Persona Integration

- domain_persona: none (no domain-expert skill installed)
