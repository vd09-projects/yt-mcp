# Skald — Project Config

<!-- rune-generated: 2026-07-06 | git: efcaded | rune: 1.0 -->

Persistence preferences for yt-mcp. Skald reads this file in Phase 0.
Skald never reads other skills' memory. Cross-skill project context lives in CLAUDE.md.

## skill_registry

Empty map = use Skald's built-in defaults (mimir=planner, sindri=implementation,
multi-perspective-review=review). Override below: no domain-expert skill installed, so
architecture artifacts route to `consumer_role: none`.

```yaml
skill_registry:
  mimir:
    producer_role: planner
    plan_types: [architecture, task]
    default_consumer_role:
      architecture: none
      task: implementation
  sindri:
    producer_role: implementation
    plan_types: [build]
    default_consumer_role:
      build: review
  multi-perspective-review:
    producer_role: review
    plan_types: [findings]
    default_consumer_role:
      findings: implementation
```

## default_owner

```
default_owner: vd
```

## slug_style

```
slug_style: kebab
slug_prefix: ""
```

## confirm_existing_match_threshold

```
confirm_existing_match_threshold: 0.7
```

## index_format

```
index_format: markdown
```

## status_overrides

```
status_overrides: {}
```
