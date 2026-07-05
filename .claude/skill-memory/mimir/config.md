# Mimir — Project Config

<!-- rune-generated: 2026-07-06 | git: efcaded | rune: 1.0 -->

Planning preferences for yt-mcp. Mimir reads this file in Phase 0.
Mimir never reads other skills' memory. Cross-skill project context lives in CLAUDE.md.

## default_depth

```
default_depth: ask
```

## domain_expert_role

```
domain_expert_role: none
```

No domain-expert skill installed. Mimir routes architecture artifacts with
`consumer_role: none` (user reads and routes manually).

## always_overlays

```
always_overlays: []
```

## never_overlays

Solo-ish project, no UI, single binary — these overlays are noise here.

```
never_overlays: [accessibility, i18n-l10n, cross-team]
```

## Notes

- v1 hardening focus. Do not plan Phase-2 (separate analytics/comments binaries) until that
  direction is decided — see CLAUDE.md "Re-run rune when".
- `auth-authz`, `concurrency`, and `observability` overlays are relevant when triggered
  (OAuth flow, in-flight locking, audit trail) — left to normal trigger matching, not forced.
