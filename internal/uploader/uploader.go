// Package uploader implements the end-to-end publish pipeline:
//
//	validate -> idempotency check (§6) -> platform guards (§5.3)
//	-> videos.insert -> thumbnails.set -> playlistItems.insert
//
// with rollback on partial failure (§7) and a full audit trail (§8).
package uploader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"

	"yt-mcp/internal/config"
	"yt-mcp/internal/store"
)

const maxThumbnailBytes = 2 * 1024 * 1024 // YouTube's custom-thumbnail limit

// Scopes requested during the one-time consent grant. youtube.upload covers
// videos.insert; force-ssl is additionally needed for thumbnails.set,
// playlistItems.insert and videos.delete (the §7 rollback path).
var Scopes = []string{
	"https://www.googleapis.com/auth/youtube.upload",
	"https://www.googleapis.com/auth/youtube.force-ssl",
}

// Uploader executes uploads against statically configured channels.
type Uploader struct {
	cfg   *config.Config
	store *store.Store
}

// New builds an Uploader.
func New(cfg *config.Config, st *store.Store) *Uploader {
	return &Uploader{cfg: cfg, store: st}
}

// service builds a per-channel YouTube client by routing to that channel's
// refresh token (static setup-time routing, spec §4.1).
func (u *Uploader) service(ctx context.Context, alias string) (*youtube.Service, error) {
	ch := u.cfg.Channels[alias]
	oc := &oauth2.Config{
		ClientID:     u.cfg.OAuth.ClientID,
		ClientSecret: u.cfg.OAuth.ClientSecret,
		Endpoint:     google.Endpoint,
		Scopes:       Scopes,
	}
	ts := oc.TokenSource(ctx, &oauth2.Token{RefreshToken: ch.RefreshToken})
	return youtube.NewService(ctx, option.WithTokenSource(ts))
}

// Upload runs the full pipeline. On failure it returns a *StageError that
// pins the failing stage and category (spec §7). Every attempt — success,
// dedup, failure, rollback — is written to the audit trail (spec §8).
func (u *Uploader) Upload(ctx context.Context, req *Request) (res *Result, retErr error) {
	started := time.Now()
	defer func() {
		entry := &store.AuditEntry{
			Channel:        req.Channel,
			IdempotencyKey: req.IdempotencyKey,
			DurationMS:     time.Since(started).Milliseconds(),
		}
		switch {
		case retErr != nil:
			entry.Outcome = "failure"
			var se *StageError
			if errors.As(retErr, &se) {
				if se.RolledBack {
					entry.Outcome = "rolled_back"
				}
				entry.Stage = se.Stage
				entry.Category = string(se.Category)
				entry.Error = se.Err.Error()
				entry.OrphanedVideoID = se.OrphanedVideoID
			} else {
				entry.Category = string(CatOther)
				entry.Error = retErr.Error()
			}
		case res != nil && res.Deduplicated:
			entry.Outcome = "deduplicated"
			entry.VideoID = res.VideoID
			entry.VideoURL = res.VideoURL
		default:
			entry.Outcome = "success"
			if res != nil {
				entry.VideoID = res.VideoID
				entry.VideoURL = res.VideoURL
			}
		}
		_ = u.store.Audit(entry)
	}()

	// ---- stage: validate ---------------------------------------------------
	ch, ok := u.cfg.Channels[req.Channel]
	if !ok {
		return nil, invalid("validate",
			"unknown channel alias %q; channels are configured statically at setup time (spec §4.1) — call list_channels to see them", req.Channel)
	}
	warnings, verr := normalize(req, ch)
	if verr != nil {
		return nil, verr
	}

	// ---- stage: idempotency_check (fast path, spec §6) -----------------------
	// A caller-supplied key that already succeeded short-circuits before we
	// spend any bandwidth resolving the source.
	if req.IdempotencyKey != "" {
		if prior, found := u.store.GetSuccess(req.IdempotencyKey); found {
			return dedupResult(prior, warnings), nil
		}
	}

	// ---- stage: resolve_source (spec §4.2: local path or remote URL) ---------
	path, sha, size, cleanup, err := resolveSource(ctx, req.Source)
	if err != nil {
		return nil, stageErr("resolve_source", err)
	}
	defer cleanup()
	if size == 0 {
		return nil, invalid("resolve_source", "video source %q resolved to 0 bytes", req.Source)
	}

	// Derived key when the caller didn't supply one: content hash + channel
	// (spec §6 allows "a content hash the tool derives").
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = fmt.Sprintf("sha256:%s@%s", sha, req.Channel)
		warnings = append(warnings, fmt.Sprintf(
			"no idempotency_key supplied; derived %q from the content hash — reuse it on retries (spec §6)", req.IdempotencyKey))
		if prior, found := u.store.GetSuccess(req.IdempotencyKey); found {
			return dedupResult(prior, warnings), nil
		}
	}

	// Reserve the key so a concurrent retry (e.g. a slow response mistaken
	// for a timeout, spec §6) cannot race this attempt into a double publish.
	if !u.store.BeginInFlight(req.IdempotencyKey) {
		return nil, &StageError{
			Stage:    "idempotency_check",
			Category: CatPolicy,
			Err:      fmt.Errorf("an upload with idempotency key %q is already in flight", req.IdempotencyKey),
			Hint:     "wait for the in-flight attempt to finish, then retry with the same key; it will be deduplicated if it succeeded",
		}
	}
	defer u.store.EndInFlight(req.IdempotencyKey)

	// ---- stage: platform_guards (spec §5.3) ----------------------------------
	if guardErr := u.platformGuards(req, sha, &warnings); guardErr != nil {
		return nil, guardErr
	}

	// ---- stage: insert_video -------------------------------------------------
	svc, err := u.service(ctx, req.Channel)
	if err != nil {
		return nil, stageErr("insert_video", fmt.Errorf("build youtube client for channel %q: %w", req.Channel, err))
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, stageErr("insert_video", fmt.Errorf("open resolved media: %w", err))
	}
	defer f.Close()

	video := &youtube.Video{
		Snippet: &youtube.VideoSnippet{
			Title:       req.Title,
			Description: req.Description,
			Tags:        req.Tags,
			CategoryId:  req.CategoryID,
		},
		Status: &youtube.VideoStatus{
			PrivacyStatus:           req.PrivacyStatus,
			SelfDeclaredMadeForKids: *req.MadeForKids,
			PublishAt:               req.PublishAt,
			// The made-for-kids declaration is required by the API (COPPA,
			// spec §4.2); force-send so an explicit `false` isn't dropped as
			// a Go zero value.
			ForceSendFields: []string{"SelfDeclaredMadeForKids"},
		},
	}

	inserted, err := svc.Videos.Insert([]string{"snippet", "status"}, video).
		Media(f, googleapi.ChunkSize(u.cfg.UploadChunkSizeMB*1024*1024)).
		Context(ctx).
		Do()
	if err != nil {
		return nil, stageErr("insert_video", err)
	}

	res = &Result{
		Channel:        req.Channel,
		VideoID:        inserted.Id,
		VideoURL:       "https://www.youtube.com/watch?v=" + inserted.Id,
		IdempotencyKey: req.IdempotencyKey,
		PrivacyStatus:  req.PrivacyStatus,
		PublishAt:      req.PublishAt,
		Warnings:       warnings,
	}
	if req.IsShort {
		res.ShortsURL = "https://www.youtube.com/shorts/" + inserted.Id
	}

	rollback := true
	if req.RollbackOnPartialFailure != nil {
		rollback = *req.RollbackOnPartialFailure
	}

	// ---- stage: set_thumbnail --------------------------------------------------
	if req.Thumbnail != "" {
		if err := u.setThumbnail(ctx, svc, inserted.Id, req.Thumbnail); err != nil {
			return u.partialFailure(ctx, svc, req, res, sha, "set_thumbnail", err, rollback)
		}
		res.ThumbnailSet = true
	}

	// ---- stage: add_to_playlist --------------------------------------------------
	if req.PlaylistID != "" {
		_, err := svc.PlaylistItems.Insert([]string{"snippet"}, &youtube.PlaylistItem{
			Snippet: &youtube.PlaylistItemSnippet{
				PlaylistId: req.PlaylistID,
				ResourceId: &youtube.ResourceId{Kind: "youtube#video", VideoId: inserted.Id},
			},
		}).Context(ctx).Do()
		if err != nil {
			return u.partialFailure(ctx, svc, req, res, sha, "add_to_playlist", err, rollback)
		}
		res.AddedToPlaylist = true
	}

	// ---- stage: finalize ---------------------------------------------------------
	if err := u.store.SaveResult(&store.Record{
		IdempotencyKey: req.IdempotencyKey,
		Channel:        req.Channel,
		Status:         store.StatusSuccess,
		VideoID:        inserted.Id,
		VideoURL:       res.VideoURL,
		ContentSHA256:  sha,
		Title:          req.Title,
		CreatedAt:      time.Now().UTC(),
	}); err != nil {
		// The video IS live. Losing the ledger entry would let a retry
		// double-publish, so surface this loudly instead of pretending all
		// is well.
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"CRITICAL: video %s was published but the idempotency ledger could not be written (%v); a retry with the same key may double-publish until this is fixed", inserted.Id, err))
	}
	return res, nil
}

// platformGuards enforces the two §5.3 risks: cross-channel duplicate
// content and timing clustering. Both are local checks — no quota cost.
func (u *Uploader) platformGuards(req *Request, sha string, warnings *[]string) *StageError {
	if dupes := u.store.FindSuccessByHash(sha); len(dupes) > 0 {
		var elsewhere []string
		for _, d := range dupes {
			if d.Channel != req.Channel {
				elsewhere = append(elsewhere, fmt.Sprintf("%s (video %s)", d.Channel, d.VideoID))
			} else if d.IdempotencyKey != req.IdempotencyKey {
				*warnings = append(*warnings, fmt.Sprintf(
					"identical content already exists on this channel as video %s (uploaded under key %q); proceeding because the idempotency keys differ", d.VideoID, d.IdempotencyKey))
			}
		}
		if len(elsewhere) > 0 {
			msg := fmt.Sprintf(
				"identical content was already published to other channel(s): %s — cross-channel duplicates are a flaggable signal independent of upload volume (spec §5.3)",
				strings.Join(elsewhere, ", "))
			if u.cfg.BlockCrossChannelDuplicates && !req.AllowCrossChannelDuplicate {
				return &StageError{
					Stage:    "platform_guards",
					Category: CatPolicy,
					Err:      errors.New(msg),
					Hint:     "pass allow_cross_channel_duplicate=true to override, or disable block_cross_channel_duplicates in the config",
				}
			}
			*warnings = append(*warnings, "WARNING: "+msg)
		}
	}

	if min := u.cfg.MinSecondsBetweenUploads; min > 0 {
		if last := u.store.LastUploadAt(); !last.IsZero() {
			if gap := time.Since(last); gap < time.Duration(min)*time.Second {
				wait := time.Duration(min)*time.Second - gap
				return &StageError{
					Stage:    "platform_guards",
					Category: CatPolicy,
					Err: fmt.Errorf(
						"upload fired %.0fs after the previous one across channels; configured minimum spacing is %ds (timing-clustering guard, spec §5.3)",
						gap.Seconds(), min),
					Hint: fmt.Sprintf("retry after ~%.0fs with the SAME idempotency key", wait.Seconds()),
				}
			}
		}
	}
	return nil
}

// setThumbnail resolves the thumbnail source (local or URL) and applies it.
func (u *Uploader) setThumbnail(ctx context.Context, svc *youtube.Service, videoID, source string) error {
	path, _, size, cleanup, err := resolveSource(ctx, source)
	if err != nil {
		return err
	}
	defer cleanup()
	if size > maxThumbnailBytes {
		return fmt.Errorf("thumbnail is %d bytes; YouTube's limit for custom thumbnails is 2 MB", size)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := svc.Thumbnails.Set(videoID).Media(f).Context(ctx).Do(); err != nil {
		var gerr *googleapi.Error
		if errors.As(err, &gerr) && (gerr.Code == 403 || gerr.Code == 401) {
			return fmt.Errorf("%w (note: custom thumbnails require a phone-verified channel, spec §4.2)", err)
		}
		return err
	}
	return nil
}

// partialFailure implements spec §7: the video row exists but a follow-up
// step failed. With rollback enabled (the default) the orphaned video is
// deleted; if the delete itself fails, the orphan is surfaced explicitly
// with the exact stage and both errors — never a generic failure message.
func (u *Uploader) partialFailure(
	ctx context.Context,
	svc *youtube.Service,
	req *Request,
	res *Result,
	sha, stage string,
	cause error,
	rollback bool,
) (*Result, error) {
	se := stageErr(stage, cause)

	if !rollback {
		// Caller opted to keep the video despite the failed step: record the
		// publish as a success (so idempotency holds) and downgrade the
		// failure to a warning in the response.
		_ = u.store.SaveResult(&store.Record{
			IdempotencyKey: req.IdempotencyKey,
			Channel:        req.Channel,
			Status:         store.StatusSuccess,
			VideoID:        res.VideoID,
			VideoURL:       res.VideoURL,
			ContentSHA256:  sha,
			Title:          req.Title,
			CreatedAt:      time.Now().UTC(),
		})
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"%s failed (%s: %v) — the video was KEPT because rollback_on_partial_failure=false; fix and re-apply that step manually", stage, se.Category, cause))
		return res, nil
	}

	if delErr := svc.Videos.Delete(res.VideoID).Context(ctx).Do(); delErr != nil {
		se.OrphanedVideoID = res.VideoID
		se.RollbackErr = delErr
		se.Hint = strings.TrimSpace(se.Hint + " — rollback also failed; delete video " + res.VideoID + " manually before retrying")
		return nil, se
	}
	se.RolledBack = true

	// Record the rollback so the ledger reflects reality; a retry with the
	// same key is allowed (only StatusSuccess short-circuits, spec §6).
	_ = u.store.SaveResult(&store.Record{
		IdempotencyKey: req.IdempotencyKey,
		Channel:        req.Channel,
		Status:         store.StatusRolledBack,
		VideoID:        res.VideoID,
		ContentSHA256:  sha,
		Title:          req.Title,
		CreatedAt:      time.Now().UTC(),
	})
	return nil, se
}

// VerifyChannel makes a cheap channels.list call (1 quota unit) to confirm a
// stored refresh token still works — particularly useful while the OAuth
// consent screen is in "Testing" status and tokens expire every 7 days
// (spec §5.1).
func (u *Uploader) VerifyChannel(ctx context.Context, alias string) (channelID, title string, err error) {
	if _, ok := u.cfg.Channels[alias]; !ok {
		return "", "", fmt.Errorf("unknown channel alias %q", alias)
	}
	svc, err := u.service(ctx, alias)
	if err != nil {
		return "", "", err
	}
	resp, err := svc.Channels.List([]string{"snippet"}).Mine(true).Context(ctx).Do()
	if err != nil {
		return "", "", err
	}
	if len(resp.Items) == 0 {
		return "", "", fmt.Errorf("token is valid but no YouTube channel is attached to the authorized account")
	}
	return resp.Items[0].Id, resp.Items[0].Snippet.Title, nil
}

// normalize validates the request against spec §4.2 and applies channel and
// global defaults. It mutates req in place and returns any warnings.
func normalize(req *Request, ch *config.Channel) ([]string, *StageError) {
	var warnings []string

	if strings.TrimSpace(req.Source) == "" {
		return nil, invalid("validate", "source is required: a local file path or an http(s) URL (spec §4.2)")
	}

	req.Title = strings.TrimSpace(req.Title)
	if se := validateTitle(req.Title); se != nil {
		return nil, se
	}

	if strings.TrimSpace(req.Description) == "" {
		return nil, invalid("validate", "description is required (spec §4.2); hashtags can live in it")
	}
	if strings.ContainsAny(req.Description, "<>") {
		return nil, invalid("validate", "description must not contain '<' or '>' (YouTube API restriction)")
	}

	if req.MadeForKids == nil {
		return nil, invalid("validate", "self_declared_made_for_kids is required by the YouTube API (COPPA, spec §4.2); pass true or false explicitly")
	}

	// Per-channel defaults from static config (spec §4.1).
	if len(req.Tags) == 0 && len(ch.DefaultTags) > 0 {
		req.Tags = append([]string(nil), ch.DefaultTags...)
	}
	if req.CategoryID == "" {
		req.CategoryID = ch.DefaultCategoryID
	}
	if req.CategoryID == "" {
		return nil, invalid("validate", "category_id is required (spec §4.2: YouTube's fixed taxonomy) and channel %q has no default_category_id", req.Channel)
	}
	if se := validateCategoryNumeric(req.CategoryID); se != nil {
		return nil, se
	}

	// Scheduled publishing (spec §4.2): requires privacy=private + a future
	// RFC3339 timestamp. Handle before privacy defaulting so we can tell
	// whether the caller explicitly set a conflicting privacy.
	if req.PublishAt != "" {
		t, err := time.Parse(time.RFC3339, req.PublishAt)
		if err != nil {
			return nil, invalid("validate", "publish_at must be RFC3339 (e.g. 2026-07-10T09:00:00+05:30): %v", err)
		}
		if !t.After(time.Now()) {
			return nil, invalid("validate", "publish_at must be in the future")
		}
		req.PublishAt = t.UTC().Format(time.RFC3339)
		switch req.PrivacyStatus {
		case "":
			req.PrivacyStatus = "private"
			warnings = append(warnings, "publish_at is set; privacy_status forced to \"private\" as scheduled publishing requires (spec §4.2)")
		case "private":
			// exactly what the API needs
		default:
			return nil, invalid("validate", "publish_at requires privacy_status=private (spec §4.2); got %q", req.PrivacyStatus)
		}
	}

	// Privacy default chain: caller -> channel default -> "unlisted".
	// Never silently public (spec §4.2).
	if req.PrivacyStatus == "" {
		req.PrivacyStatus = ch.DefaultPrivacy
	}
	if req.PrivacyStatus == "" {
		req.PrivacyStatus = "unlisted"
		warnings = append(warnings, "privacy_status not provided; defaulted to \"unlisted\" — this tool never silently defaults to public (spec §4.2)")
	}
	if !config.ValidPrivacy(req.PrivacyStatus) {
		return nil, invalid("validate", "privacy_status must be one of public, unlisted, private; got %q", req.PrivacyStatus)
	}

	// Shorts (spec §4.3): explicit caller flag, deterministic behavior. The
	// tool never infers; it only reinforces YouTube's own detection
	// (vertical aspect ratio + duration) with a #Shorts marker.
	if req.IsShort && !containsFold(req.Description, "#shorts") {
		req.Description += "\n\n#Shorts"
		warnings = append(warnings, "is_short=true: appended #Shorts to the description to reinforce YouTube's Shorts detection (spec §4.3)")
	}

	if len(req.Description) > 5000 {
		return nil, invalid("validate", "description exceeds YouTube's 5000-byte limit (%d bytes)", len(req.Description))
	}

	// Tags share a combined 500-character budget server-side, counted in
	// characters (runes), not bytes. Reject over-budget sets before
	// videos.insert as defense-in-depth / fast-fail; YouTube remains the
	// enforcing boundary. The tool never auto-trims — fail loud (spec §7).
	// Shared with the edit path via validateTagsBudget.
	if se := validateTagsBudget(req.Tags); se != nil {
		return nil, se
	}

	return warnings, nil
}

// tagsBudget computes YouTube's combined tag budget in characters (runes, not
// bytes): each tag's character count, plus 1 comma between adjacent tags, plus
// 2 for the quotes YouTube wraps around any tag containing a space. The 500
// limit is enforced by the caller. Pure helper, isolated for table testing.
func tagsBudget(tags []string) int {
	total := 0
	for _, t := range tags {
		total += utf8.RuneCountInString(t)
		// NOTE: strings.Contains(t, " ") only detects the ASCII space, so a
		// tag whose only whitespace is a tab is under-counted by 2. Known
		// minor under-count, left as-is — YouTube is the enforcing boundary.
		if strings.Contains(t, " ") {
			total += 2
		}
	}
	if n := len(tags); n > 1 {
		total += n - 1
	}
	return total
}

// dedupResult converts a prior ledger record into a Result (spec §6: return
// the existing result instead of re-publishing).
func dedupResult(prior *store.Record, warnings []string) *Result {
	return &Result{
		Channel:        prior.Channel,
		VideoID:        prior.VideoID,
		VideoURL:       prior.VideoURL,
		IdempotencyKey: prior.IdempotencyKey,
		Deduplicated:   true,
		Warnings: append(warnings, fmt.Sprintf(
			"idempotency key %q already has a successful upload from %s; returning the existing result instead of re-publishing (spec §6)",
			prior.IdempotencyKey, prior.CreatedAt.Format(time.RFC3339))),
	}
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
