package uploader

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"google.golang.org/api/youtube/v3"
)

// Pipeline stages for the edit path. No new Category values are needed — the
// existing Categorize/stageErr taxonomy covers every failure; only the stage
// names are new.
const (
	stageValidate    = "validate"
	stageFetchVideo  = "fetch_video"
	stageUpdateVideo = "update_video"
)

// EditRequest is one caller-specified metadata correction for an existing
// video. Every editable snippet field is a pointer so three intents are
// distinguishable, which a plain string cannot express:
//
//	nil                      -> preserve the current value
//	non-nil, non-empty       -> overwrite with the new value
//	non-nil, empty ("" / [])  -> CLEAR the field (force-sent on the wire)
//
// Title and CategoryID are required by videos.update and therefore cannot be
// cleared; a request that would clear either is rejected before any API call.
// The edit path targets a caller-supplied VideoID: it mutates an existing
// resource and creates nothing, so it never touches the idempotency ledger or
// the in-flight lock (those guard videos.insert against double-publish).
type EditRequest struct {
	Channel         string
	VideoID         string
	Title           *string
	Description     *string
	Tags            *[]string
	CategoryID      *string
	DefaultLanguage *string
}

// EditResult is the successful outcome of an edit. UpdatedFields lists the
// snippet fields whose value actually changed (including intentional clears);
// omitted or unchanged fields are not listed.
type EditResult struct {
	Channel       string
	VideoID       string
	VideoURL      string
	UpdatedFields []string
	Warnings      []string
}

// videoService is the minimal, package-local seam over exactly the two calls
// the edit path makes. It exists so the ForceSendFields -> videos.update wire
// contract is observable in a test without a live YouTube call. It is
// unexported and adds no public / SSRF / token surface.
type videoService interface {
	List(ctx context.Context, id string) (*youtube.VideoListResponse, error)
	Update(ctx context.Context, video *youtube.Video) (*youtube.Video, error)
}

// ytVideoService is the production adapter: a thin, behaviour-preserving
// forward over the *youtube.Service that u.service already builds. Each method
// is a single forwarding call so the fake in tests cannot drift far from it.
type ytVideoService struct{ svc *youtube.Service }

func (s *ytVideoService) List(ctx context.Context, id string) (*youtube.VideoListResponse, error) {
	return s.svc.Videos.List([]string{"snippet"}).Id(id).Context(ctx).Do()
}

func (s *ytVideoService) Update(ctx context.Context, video *youtube.Video) (*youtube.Video, error) {
	return s.svc.Videos.Update([]string{"snippet"}, video).Context(ctx).Do()
}

// EditMetadata edits the snippet metadata of an existing video via a YouTube
// Data API v3 read-modify-write: videos.list(snippet) -> mutate -> videos.update
// (snippet). videos.update REPLACES the whole snippet, so the merge always
// starts from the fetched current value; see mergeSnippet for preserve-vs-clear.
// Quota: list (1u) + update (50u) = ~51 units/edit, separate from upload quota.
//
// Observability: v1 edits are UNAUDITED. store.AuditEntry has no changed_fields
// member, so an audit record could not say WHICH fields an edit changed without
// a schema change — which is out of scope for v1 (plan Observability decision).
// This method writes neither the audit log nor the ledger.
//
// TODO: add changed_fields []string to store.AuditEntry so edits can be audited (tracked: #15)
// in internal/store/store.go — deferred as an out-of-scope schema change.
func (u *Uploader) EditMetadata(ctx context.Context, req *EditRequest) (*EditResult, error) {
	// ---- stage: validate (request-level, before ANY API call) --------------
	if _, ok := u.cfg.Channels[req.Channel]; !ok {
		return nil, invalid(stageValidate,
			"unknown channel alias %q; channels are configured statically at setup time (spec §4.1) — call list_channels to see them", req.Channel)
	}
	if strings.TrimSpace(req.VideoID) == "" {
		return nil, invalid(stageValidate, "video_id is required")
	}
	// Reject clearing a required field before spending any quota on the fetch.
	if se := rejectClearRequired(req); se != nil {
		return nil, se
	}

	svc, err := u.service(ctx, req.Channel)
	if err != nil {
		return nil, stageErr(stageUpdateVideo, fmt.Errorf("build youtube client for channel %q: %w", req.Channel, err))
	}
	return editMetadata(ctx, &ytVideoService{svc: svc}, req)
}

// editMetadata runs the fetch -> merge -> update flow against an injected
// videoService so the whole read-modify-write is testable off the network.
func editMetadata(ctx context.Context, vs videoService, req *EditRequest) (*EditResult, error) {
	// ---- stage: fetch_video ------------------------------------------------
	resp, err := vs.List(ctx, req.VideoID)
	if err != nil {
		return nil, stageErr(stageFetchVideo, err)
	}
	if len(resp.Items) == 0 || resp.Items[0].Snippet == nil {
		se := invalid(stageFetchVideo, "no video with id %q is visible to channel %q", req.VideoID, req.Channel)
		se.Hint = "the id may be wrong, the video may not belong to (or be visible to) this channel, or it may have been deleted"
		return nil, se
	}
	current := resp.Items[0].Snippet

	// ---- stage: validate (merge + validate the merged snippet) -------------
	merged, updated, mErr := mergeSnippet(current, req)
	if mErr != nil {
		return nil, mErr
	}

	// ---- stage: update_video -----------------------------------------------
	// videos.update 403s if the routed channel does not own the video;
	// Categorize maps that to CatAuth, which is the correct, informative
	// failure — no extra ownership pre-check is needed.
	if _, err := vs.Update(ctx, &youtube.Video{Id: req.VideoID, Snippet: merged}); err != nil {
		return nil, stageErr(stageUpdateVideo, err)
	}

	return &EditResult{
		Channel:       req.Channel,
		VideoID:       req.VideoID,
		VideoURL:      "https://www.youtube.com/watch?v=" + req.VideoID,
		UpdatedFields: updated,
	}, nil
}

// rejectClearRequired rejects a request that would clear title or categoryId
// (present-but-empty). Callable with only the request, so EditMetadata can fail
// fast before the fetch; mergeSnippet calls it too so the pure merge is
// self-guarding.
func rejectClearRequired(req *EditRequest) *StageError {
	if req.Title != nil && strings.TrimSpace(*req.Title) == "" {
		return invalid(stageValidate, "title is required and cannot be cleared; omit the field to preserve it")
	}
	if req.CategoryID != nil && strings.TrimSpace(*req.CategoryID) == "" {
		return invalid(stageValidate, "category_id is required and cannot be cleared; omit the field to preserve it")
	}
	return nil
}

// mergeSnippet is the pure core of the edit path. Starting from the fetched
// current snippet (so every untouched field is preserved by construction), it
// applies the caller's pointer-encoded preserve/overwrite/clear intent,
// accumulates ForceSendFields for explicit clears, validates the merged result
// with the shared validators, and computes UpdatedFields (merged value differs
// from current, including intentional clears). It never mutates current.
func mergeSnippet(current *youtube.VideoSnippet, edit *EditRequest) (*youtube.VideoSnippet, []string, *StageError) {
	if se := rejectClearRequired(edit); se != nil {
		return nil, nil, se
	}

	merged := *current // shallow copy: read-only fields (channelId, channelTitle,
	m := &merged       // thumbnails, publishedAt, liveBroadcastContent, …) ride along.
	m.ForceSendFields = nil
	// Do not send snippet.localized: YouTube derives it from defaultLanguage +
	// the base title/description, and a stale block conflicts. nil => omitted
	// (not force-cleared) so the server reconciles it.
	m.Localized = nil

	var forceSend []string

	if edit.Title != nil { // guaranteed non-empty by rejectClearRequired
		m.Title = strings.TrimSpace(*edit.Title)
	}
	if edit.Description != nil {
		if *edit.Description == "" {
			m.Description = ""
			forceSend = append(forceSend, "Description")
		} else {
			m.Description = *edit.Description
		}
	}
	if edit.Tags != nil {
		if len(*edit.Tags) == 0 {
			m.Tags = []string{}
			forceSend = append(forceSend, "Tags")
		} else {
			m.Tags = append([]string(nil), (*edit.Tags)...)
		}
	}
	if edit.CategoryID != nil { // guaranteed non-empty by rejectClearRequired
		m.CategoryId = strings.TrimSpace(*edit.CategoryID)
	}
	if edit.DefaultLanguage != nil {
		if *edit.DefaultLanguage == "" {
			m.DefaultLanguage = ""
			forceSend = append(forceSend, "DefaultLanguage")
		} else {
			m.DefaultLanguage = *edit.DefaultLanguage
		}
	}

	// Validate the merged result with the SAME shared validators the upload
	// path uses (title rules, numeric category, 500-char rune tag budget).
	if se := validateTitle(m.Title); se != nil {
		return nil, nil, se
	}
	if se := validateCategoryNumeric(m.CategoryId); se != nil {
		return nil, nil, se
	}
	if se := validateTagsBudget(m.Tags); se != nil {
		return nil, nil, se
	}

	m.ForceSendFields = forceSend

	// UpdatedFields: merged value differs from current, incl. intentional
	// clears. A field cleared that was already empty is force-sent for
	// wire-safety but NOT listed here — ForceSendFields and UpdatedFields are
	// deliberately different sets. Names are caller-facing.
	var updated []string
	if m.Title != current.Title {
		updated = append(updated, "title")
	}
	if m.Description != current.Description {
		updated = append(updated, "description")
	}
	if !equalStrings(m.Tags, current.Tags) {
		updated = append(updated, "tags")
	}
	if m.CategoryId != current.CategoryId {
		updated = append(updated, "categoryId")
	}
	if m.DefaultLanguage != current.DefaultLanguage {
		updated = append(updated, "defaultLanguage")
	}

	return m, updated, nil
}

// equalStrings reports whether two string slices are element-wise equal; a nil
// slice and an empty slice are treated as equal (both "no tags").
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---- shared shape validators (used by both normalize and the edit path) ----
//
// These are pure and message-stable so the upload path's behaviour is
// preserved byte-for-byte when normalize delegates to them.

// validateTitle enforces the create/edit title rules: non-empty, ≤ 100 runes
// (not bytes), no '<' or '>'.
func validateTitle(title string) *StageError {
	if strings.TrimSpace(title) == "" {
		return invalid(stageValidate, "title is required (spec §4.2)")
	}
	if utf8.RuneCountInString(title) > 100 {
		return invalid(stageValidate, "title exceeds YouTube's 100-character limit (%d chars)", utf8.RuneCountInString(title))
	}
	if strings.ContainsAny(title, "<>") {
		return invalid(stageValidate, "title must not contain '<' or '>' (YouTube API restriction)")
	}
	return nil
}

// validateCategoryNumeric enforces that a (already-required) category id is a
// numeric YouTube category id.
func validateCategoryNumeric(categoryID string) *StageError {
	if _, err := strconv.Atoi(categoryID); err != nil {
		return invalid(stageValidate, "category_id must be a numeric YouTube category id, e.g. \"22\" (People & Blogs) or \"28\" (Science & Technology); got %q", categoryID)
	}
	return nil
}

// validateTagsBudget rejects tag sets over YouTube's 500-character combined
// budget (rune-counted via tagsBudget). Never stricter than the server.
func validateTagsBudget(tags []string) *StageError {
	if budget := tagsBudget(tags); budget > 500 {
		se := invalid(stageValidate, "tags exceed YouTube's 500-character combined limit (%d characters)", budget)
		se.Hint = "the combined budget counts each tag's characters + 1 comma between adjacent tags + 2 for the quotes YouTube adds around any tag containing a space; shorten or drop tags to fit within 500 characters (the tool will not auto-trim for you)"
		return se
	}
	return nil
}
