# Skald — Scope Registry

<!-- rune-generated: 2026-07-06 | git: efcaded | rune: 1.0 -->

Append-only registry of every scope Skald has touched in this project. Maintained by
Skald — humans should not edit freehand except for slug-rename operations.

---

```yaml
scopes:
  - slug: edit-video-metadata-tool
    title: Add videos.update edit_video_metadata MCP tool
    created: 2026-07-08
    created_by: vd
    reasoning: |
      Kebab-case noun phrase naming the deliverable — a fourth MCP tool that
      edits an existing video's snippet metadata via videos.update. Chosen over
      "edit-metadata" (too generic — could read as config/channel metadata) and
      "videos-update-tool" (leaks the API method name, not the capability). The
      slug centers the user-facing verb (edit video metadata) and stays ≤40
      chars. Distinct from tags-validation-docs (#11, upload-path tag guard);
      this is the separate edit-metadata capability (was referenced there as #10).
    status: active
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
