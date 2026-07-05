# Skald — Scope Registry

<!-- rune-generated: 2026-07-06 | git: efcaded | rune: 1.0 -->

Append-only registry of every scope Skald has touched in this project. Maintained by
Skald — humans should not edit freehand except for slug-rename operations.

---

```yaml
scopes: []
```

---

## Field definitions

| Field | Required | Notes |
|---|---|---|
| `slug` | yes | Kebab-case, lowercase, ≤40 chars, alphanumeric + dashes. Matches the scope dir name. |
| `title` | yes | One-line title for INDEX.md display. |
| `created` | yes | ISO date (YYYY-MM-DD) of first creation. |
| `created_by` | yes | Skald's `default_owner` config value, or whatever the user supplied. |
| `reasoning` | yes | Multi-line explanation of WHY this slug over alternatives. |
| `aliases` | no | Other slugs Skald should treat as equivalent. |
| `status` | yes | `active` | `done` | `archived`. |
