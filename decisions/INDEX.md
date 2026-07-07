# Decision Index

<!-- Newest first. Machine-friendly YAML entries. -->

- id: 2026-07-08-edit-path-off-idempotency-ledger
  title: "Edit path is kept off the idempotency ledger and in-flight lock"
  date: 2026-07-08
  status: accepted
  category: architecture
  tags: [youtube, videos-update, idempotency, ledger, edit-metadata, uploader, issue-10]
  path: architecture/2026-07-08-edit-path-off-idempotency-ledger.md
  summary: "edit_video_metadata (#10) deliberately does not touch the idempotency ledger or in-flight lock. An edit targets a caller-supplied video_id and mutates an existing resource — it cannot mint a duplicate, so there is no double-publish invariant to protect; injecting edit records would corrupt create-dedup (a later upload could misread an edit record as a prior upload success)."

- id: 2026-07-08-edits-unaudited-in-v1
  title: "Edits are unaudited in v1 (no changed_fields in the audit schema)"
  date: 2026-07-08
  status: accepted
  category: tradeoff
  tags: [youtube, audit, edit-metadata, uploader, deferred, todo, issue-10]
  path: tradeoff/2026-07-08-edits-unaudited-in-v1.md
  summary: "store.AuditEntry has no changed_fields member and adding one is an out-of-scope schema change, so EditMetadata writes neither the audit log nor the ledger in v1. A // TODO in edit.go plus a follow-up issue track adding AuditEntry.changed_fields later, aligned to the caller-facing names UpdatedFields already emits."

- id: 2026-07-08-pointer-based-three-valued-edit-field-semantics
  title: "Pointer-based three-valued field semantics for edit requests"
  date: 2026-07-08
  status: accepted
  category: design
  tags: [youtube, videos-update, edit-metadata, pointers, forcesendfields, three-valued, uploader, issue-10]
  path: design/2026-07-08-pointer-based-three-valued-edit-field-semantics.md
  summary: "*string / *[]string with omitempty encode preserve (nil), overwrite (non-nil non-empty), and clear (non-nil empty). Intentional clears survive Google's generated client via snippet.ForceSendFields — the same force-send technique the insert path uses for *bool SelfDeclaredMadeForKids. title and categoryId are non-clearable and rejected before any API call."

- id: 2026-07-08-videoservice-interface-forcesendfields-on-wire
  title: "videoService interface asserts ForceSendFields reaches the wire"
  date: 2026-07-08
  status: accepted
  category: design
  tags: [youtube, videos-update, testing, forcesendfields, seam, fake, edit-metadata, uploader, issue-10]
  path: design/2026-07-08-videoservice-interface-forcesendfields-on-wire.md
  summary: "An unexported package-local videoService interface (List + Update) lets a fake capture the outgoing snippet and assert ForceSendFields on the wire, not just at mergeSnippet's return — closing the class of bug where a merge is correct in memory but the force-send list is dropped before serialization. Convention: any videos.update / partial-update write should test that ForceSendFields/NullFields reach the wire."

- id: 2026-07-08-snippet-localized-nil-server-reconcile
  title: "snippet.localized set to nil so the server reconciles it"
  date: 2026-07-08
  status: accepted
  category: design
  tags: [youtube, videos-update, localized, merge-snippet, edit-metadata, uploader, issue-10]
  path: design/2026-07-08-snippet-localized-nil-server-reconcile.md
  summary: "mergeSnippet sets snippet.localized to nil on the merged snippet so the server reconciles it from the base title/description/defaultLanguage rather than round-tripping a possibly-stale localized block."

- id: 2026-07-08-shared-shape-validators-extracted
  title: "Shared shape-validators extracted from normalize and reused by the edit path"
  date: 2026-07-08
  status: accepted
  category: tradeoff
  tags: [youtube, validation, refactor, dry, edit-metadata, uploader, parity-test, issue-10]
  path: tradeoff/2026-07-08-shared-shape-validators-extracted.md
  summary: "validateTitle, validateCategoryNumeric, validateTagsBudget were extracted from normalize and reused by the edit path rather than duplicated inline behind a parity test — divergence between the two validation paths was the most likely future bug. The extraction is behavior-preserving (messages/stage/category byte-identical, upload tests unchanged); a parity test is retained as cheap redundancy."

- id: 2026-07-08-last-writer-wins-no-etag-concurrency
  title: "Last-writer-wins accepted; no ETag/If-Match optimistic concurrency in v1"
  date: 2026-07-08
  status: accepted
  category: tradeoff
  tags: [youtube, videos-update, concurrency, etag, lost-update, edit-metadata, uploader, issue-10]
  path: tradeoff/2026-07-08-last-writer-wins-no-etag-concurrency.md
  summary: "Last-writer-wins / lost-update is an accepted characteristic of this single-operator correction utility; v1 ships no ETag/If-Match optimistic concurrency. A concurrent edit between fetch and update can be silently overwritten — acceptable given the single-operator model."

- id: 2026-07-08-tag-validation-counts-characters-runes-not-bytes
  title: "Tag validation counts characters (runes), not bytes"
  date: 2026-07-08
  status: accepted
  category: tradeoff
  tags: [youtube, tags, validation, unicode, runes, client-guard, uploader, issue-11]
  path: tradeoff/2026-07-08-tag-validation-counts-characters-runes-not-bytes.md
  summary: "Switched the snippet.tags 500-limit guard from bytes (len) to characters (utf8.RuneCountInString) via a pure tagsBudget helper. A client guard must never be stricter than the server; rune counting matches the documented 500 characters and only widens acceptance (ASCII identical). Shipped with the first internal/uploader table test. Implements #11 under the 'tags not a Shorts lever' decision."

- id: 2026-07-08-tags-not-a-shorts-discovery-lever
  title: "Tags are not a Shorts discovery lever"
  date: 2026-07-08
  status: accepted
  category: tradeoff
  tags: [youtube, shorts, tags, discovery, metadata, videos-update, research-9]
  path: tradeoff/2026-07-08-tags-not-a-shorts-discovery-lever.md
  summary: "Verified tags don't meaningfully drive Shorts views; keep tags-at-upload as-is, invest in engagement + title/description/hashtags, add videos.update only as a correction utility. From research #9."
