# Decision Index

<!-- Newest first. Machine-friendly YAML entries. -->

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
