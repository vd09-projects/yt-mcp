# Rune Configuration

**Last updated:** 2026-07-06

## Backend

```
backend: github
```

GitHub repo: `vd09-projects/yt-mcp`. Issues stored as `gh` issues; task ID = issue number.

## Default mode

```
default_mode: vibe
```

The core pipeline (config → resolve source → videos.insert → thumbnail → playlist,
with idempotency + rollback) is already scaffolded. Steady-state work is atomic edits
within those defined seams: validation rules, error categorization, guard tuning, test
coverage. Interface/scaffold-level work (new tools, new persistence seams) overrides
per task to `rune:dev`.

## Sizing rubric

| Rune | Solves | Output | Sizing | Forbidden |
|---|---|---|---|---|
| **dev** | Big chunk of the problem; an end-to-end feature slice. | Shipped code + tests + integration. | 3-4 days of focused work. | Splitting itself into vibe siblings to dodge review weight. |
| **vibe** | One subchunk of an already-understood problem. | Concrete code edit, atomic and reviewable. | Hours, one focused diff. | Interfaces, scaffolding, speculative abstractions, "set up the structure for". |
| **research** | Unknown — how does X work, what library to use, what does the API return. | Written findings or decision-journal entry. | Bounded timebox. | Shipping production code. |
| **analysis** | Best approach unclear — known problem, unknown solution. | Tradeoff comparison + recommendation. | Bounded timebox. | Shipping production code. |

## Exceptions

- Security-sensitive changes (OAuth flow, token handling, SSRF surface in `resolveSource`)
  always carry a `multi-perspective-review` pass regardless of rune size.
