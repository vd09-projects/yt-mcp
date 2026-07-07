// Package mcptool exposes the uploader over MCP. Four tools are registered:
//
//   - upload_video        — the core capability (spec §1)
//   - list_channels       — lets the calling application discover the statically
//     configured channel aliases and their defaults (spec §4.1)
//   - verify_channels     — cheap token health check (1 quota unit/channel);
//     useful while Testing-mode refresh tokens expire every 7 days (spec §5.1)
//   - edit_video_metadata — correct the snippet metadata of an EXISTING video
//     (title/description/tags/category/default language) via a read-modify-write
package mcptool

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"yt-mcp/internal/config"
	"yt-mcp/internal/uploader"
)

// UploadInput mirrors the spec §4.2 input table.
type UploadInput struct {
	Channel                    string   `json:"channel" jsonschema:"channel alias to publish to; must be one of the statically pre-configured channels (call list_channels to discover them)"`
	Source                     string   `json:"source" jsonschema:"video source: an absolute local file path or an http(s) URL (e.g. a GitHub-hosted file)"`
	Title                      string   `json:"title" jsonschema:"video title, max 100 characters, must not contain < or >"`
	Description                string   `json:"description" jsonschema:"video description, max 5000 bytes; hashtags can live here"`
	Tags                       []string `json:"tags,omitempty" jsonschema:"optional backend keyword tags, distinct from description hashtags; combined 500-character budget (each tag's characters + 1 comma between tags + 2 for quotes around any spaced tag)"`
	CategoryID                 string   `json:"category_id,omitempty" jsonschema:"numeric YouTube category id from the fixed taxonomy (e.g. 28 = Science & Technology); falls back to the channel's configured default"`
	PrivacyStatus              string   `json:"privacy_status,omitempty" jsonschema:"public, unlisted or private; if omitted defaults to the channel default and then to unlisted — never silently public"`
	SelfDeclaredMadeForKids    *bool    `json:"self_declared_made_for_kids" jsonschema:"REQUIRED COPPA declaration demanded by the YouTube API; pass true or false explicitly"`
	Thumbnail                  string   `json:"thumbnail,omitempty" jsonschema:"optional custom thumbnail, local path or URL, max 2 MB; requires a phone-verified channel"`
	PlaylistID                 string   `json:"playlist_id,omitempty" jsonschema:"optional playlist to add the video to after upload"`
	PublishAt                  string   `json:"publish_at,omitempty" jsonschema:"optional RFC3339 scheduled publish time; requires (or forces, if privacy omitted) privacy_status=private"`
	IsShort                    bool     `json:"is_short,omitempty" jsonschema:"set true when uploading a Short; there is no Shorts endpoint, so the caller declares it explicitly and the tool reinforces with #Shorts rather than inferring"`
	IdempotencyKey             string   `json:"idempotency_key,omitempty" jsonschema:"caller-supplied idempotency key; if omitted the tool derives sha256(content)@channel. ALWAYS reuse the same key when retrying so a retry never double-publishes"`
	RollbackOnPartialFailure   *bool    `json:"rollback_on_partial_failure,omitempty" jsonschema:"default true: if the thumbnail or playlist step fails after the video uploaded, delete the video (rollback); set false to keep it and receive a warning instead"`
	AllowCrossChannelDuplicate bool     `json:"allow_cross_channel_duplicate,omitempty" jsonschema:"override the cross-channel duplicate-content block when the server config enables it"`
}

// UploadOutput is the structured result for both success and failure. On
// failure it carries the spec §7 fields: stage, category, and rollback state.
type UploadOutput struct {
	Status          string   `json:"status" jsonschema:"success, deduplicated or error"`
	Channel         string   `json:"channel,omitempty"`
	VideoID         string   `json:"video_id,omitempty"`
	VideoURL        string   `json:"video_url,omitempty"`
	ShortsURL       string   `json:"shorts_url,omitempty"`
	IdempotencyKey  string   `json:"idempotency_key,omitempty"`
	PrivacyStatus   string   `json:"privacy_status,omitempty"`
	PublishAt       string   `json:"publish_at,omitempty"`
	ThumbnailSet    bool     `json:"thumbnail_set,omitempty"`
	AddedToPlaylist bool     `json:"added_to_playlist,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`

	Stage           string `json:"stage,omitempty" jsonschema:"pipeline stage at which the failure occurred"`
	Category        string `json:"category,omitempty" jsonschema:"auth_error | quota_exceeded | invalid_request | network_error | policy_violation | other"`
	Error           string `json:"error,omitempty"`
	Hint            string `json:"hint,omitempty"`
	RolledBack      bool   `json:"rolled_back,omitempty" jsonschema:"true if a partially-completed upload was deleted"`
	OrphanedVideoID string `json:"orphaned_video_id,omitempty" jsonschema:"set when rollback was attempted but the delete failed, leaving a live video"`
}

// EditVideoMetadataInput edits the snippet metadata of an existing video. Each
// editable field is a pointer: OMITTING it preserves the current value, passing
// an empty value CLEARS it. Title and category_id are required and cannot be
// cleared.
type EditVideoMetadataInput struct {
	Channel         string    `json:"channel" jsonschema:"channel alias whose OAuth token owns the video; must be one of the statically pre-configured channels (call list_channels). The caller chooses the channel — the tool never picks it."`
	VideoID         string    `json:"video_id" jsonschema:"id of the existing video to edit; the video must belong to (and be visible to) the routed channel"`
	Title           *string   `json:"title,omitempty" jsonschema:"new title (max 100 characters, no < or >). OMIT the field to preserve the current title. Title is REQUIRED and cannot be cleared."`
	Description     *string   `json:"description,omitempty" jsonschema:"new description. OMIT the field to preserve the current description; pass an empty string to CLEAR it."`
	Tags            *[]string `json:"tags,omitempty" jsonschema:"replacement backend keyword tags (combined 500-character budget). OMIT the field to preserve current tags; pass an empty array to CLEAR all tags. Tags are near-useless for discovery — this does not boost views."`
	CategoryID      *string   `json:"category_id,omitempty" jsonschema:"new numeric YouTube category id from the fixed taxonomy. OMIT the field to preserve the current category. Category is REQUIRED and cannot be cleared."`
	DefaultLanguage *string   `json:"default_language,omitempty" jsonschema:"new BCP-47 default language for the title/description. OMIT the field to preserve it; pass an empty string to CLEAR it."`
}

// EditVideoMetadataOutput is the structured result for both success and
// failure. On failure it carries the pipeline stage and category.
type EditVideoMetadataOutput struct {
	Status        string   `json:"status" jsonschema:"success or error"`
	Channel       string   `json:"channel,omitempty"`
	VideoID       string   `json:"video_id,omitempty"`
	VideoURL      string   `json:"video_url,omitempty"`
	UpdatedFields []string `json:"updated_fields,omitempty" jsonschema:"snippet fields whose value actually changed, including intentional clears; omitted or unchanged fields are not listed"`
	Warnings      []string `json:"warnings,omitempty"`

	Stage    string `json:"stage,omitempty" jsonschema:"pipeline stage at which the failure occurred: validate | fetch_video | update_video"`
	Category string `json:"category,omitempty" jsonschema:"auth_error | quota_exceeded | invalid_request | network_error | policy_violation | other"`
	Error    string `json:"error,omitempty"`
	Hint     string `json:"hint,omitempty"`
}

// ListChannelsInput is empty; the tool takes no arguments.
type ListChannelsInput struct{}

// ChannelInfo is one configured channel, secrets omitted.
type ChannelInfo struct {
	Alias             string   `json:"alias"`
	Description       string   `json:"description,omitempty"`
	DefaultCategoryID string   `json:"default_category_id,omitempty"`
	DefaultPrivacy    string   `json:"default_privacy,omitempty"`
	DefaultTags       []string `json:"default_tags,omitempty"`
}

// ListChannelsOutput lists the statically configured channels.
type ListChannelsOutput struct {
	Channels []ChannelInfo `json:"channels"`
}

// VerifyChannelsInput optionally narrows verification to one alias.
type VerifyChannelsInput struct {
	Channel string `json:"channel,omitempty" jsonschema:"optionally verify a single channel alias; when omitted all configured channels are verified"`
}

// ChannelVerification is the health-check result for one channel.
type ChannelVerification struct {
	Alias     string `json:"alias"`
	OK        bool   `json:"ok"`
	ChannelID string `json:"channel_id,omitempty"`
	Title     string `json:"title,omitempty"`
	Error     string `json:"error,omitempty"`
	Category  string `json:"category,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// VerifyChannelsOutput aggregates verification results.
type VerifyChannelsOutput struct {
	Results []ChannelVerification `json:"results"`
}

// Register wires the three tools onto the MCP server.
func Register(server *mcp.Server, up *uploader.Uploader, cfg *config.Config) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "upload_video",
		Description: "Publish a video or Short to one of the pre-configured YouTube channels via the YouTube Data API v3. " +
			"The caller decides the channel and all metadata; this tool only executes. " +
			"Idempotent: pass the same idempotency_key on retries and an already-successful upload returns the existing result instead of re-publishing. " +
			"privacy_status defaults to unlisted — never silently public. " +
			"On failure returns a structured error with the exact pipeline stage and a category (auth_error, quota_exceeded, invalid_request, network_error, policy_violation, other).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in UploadInput) (*mcp.CallToolResult, UploadOutput, error) {
		req := &uploader.Request{
			Channel:                    in.Channel,
			Source:                     in.Source,
			Title:                      in.Title,
			Description:                in.Description,
			Tags:                       in.Tags,
			CategoryID:                 in.CategoryID,
			PrivacyStatus:              in.PrivacyStatus,
			MadeForKids:                in.SelfDeclaredMadeForKids,
			Thumbnail:                  in.Thumbnail,
			PlaylistID:                 in.PlaylistID,
			PublishAt:                  in.PublishAt,
			IsShort:                    in.IsShort,
			IdempotencyKey:             in.IdempotencyKey,
			RollbackOnPartialFailure:   in.RollbackOnPartialFailure,
			AllowCrossChannelDuplicate: in.AllowCrossChannelDuplicate,
		}

		res, err := up.Upload(ctx, req)
		if err != nil {
			out := UploadOutput{Status: "error", Channel: in.Channel, IdempotencyKey: req.IdempotencyKey}
			var se *uploader.StageError
			if errors.As(err, &se) {
				out.Stage = se.Stage
				out.Category = string(se.Category)
				out.Error = se.Err.Error()
				out.Hint = se.Hint
				out.RolledBack = se.RolledBack
				out.OrphanedVideoID = se.OrphanedVideoID
			} else {
				out.Category = string(uploader.CatOther)
				out.Error = err.Error()
			}
			return asResult(out, true), out, nil
		}

		out := UploadOutput{
			Status:          "success",
			Channel:         res.Channel,
			VideoID:         res.VideoID,
			VideoURL:        res.VideoURL,
			ShortsURL:       res.ShortsURL,
			IdempotencyKey:  res.IdempotencyKey,
			PrivacyStatus:   res.PrivacyStatus,
			PublishAt:       res.PublishAt,
			ThumbnailSet:    res.ThumbnailSet,
			AddedToPlaylist: res.AddedToPlaylist,
			Warnings:        res.Warnings,
		}
		if res.Deduplicated {
			out.Status = "deduplicated"
		}
		return asResult(out, false), out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "list_channels",
		Description: "List the statically pre-configured YouTube channel aliases this server can publish to, " +
			"with their per-channel defaults (category, privacy, tags). Channel routing is setup-time configuration; " +
			"the calling application chooses among these aliases.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ ListChannelsInput) (*mcp.CallToolResult, ListChannelsOutput, error) {
		aliases := make([]string, 0, len(cfg.Channels))
		for alias := range cfg.Channels {
			aliases = append(aliases, alias)
		}
		sort.Strings(aliases)

		out := ListChannelsOutput{Channels: make([]ChannelInfo, 0, len(aliases))}
		for _, alias := range aliases {
			ch := cfg.Channels[alias]
			out.Channels = append(out.Channels, ChannelInfo{
				Alias:             alias,
				Description:       ch.Description,
				DefaultCategoryID: ch.DefaultCategoryID,
				DefaultPrivacy:    ch.DefaultPrivacy,
				DefaultTags:       ch.DefaultTags,
			})
		}
		return asResult(out, false), out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "verify_channels",
		Description: "Verify that each configured channel's OAuth refresh token still works, via a 1-quota-unit channels.list call. " +
			"Run this before a batch of uploads: while the OAuth consent screen is in Testing status, refresh tokens expire every 7 days.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in VerifyChannelsInput) (*mcp.CallToolResult, VerifyChannelsOutput, error) {
		aliases := make([]string, 0, len(cfg.Channels))
		if in.Channel != "" {
			aliases = append(aliases, in.Channel)
		} else {
			for alias := range cfg.Channels {
				aliases = append(aliases, alias)
			}
			sort.Strings(aliases)
		}

		out := VerifyChannelsOutput{}
		anyFailed := false
		for _, alias := range aliases {
			v := ChannelVerification{Alias: alias}
			id, title, err := up.VerifyChannel(ctx, alias)
			if err != nil {
				anyFailed = true
				cat, hint := uploader.Categorize(err)
				v.Error = err.Error()
				v.Category = string(cat)
				v.Hint = hint
			} else {
				v.OK = true
				v.ChannelID = id
				v.Title = title
			}
			out.Results = append(out.Results, v)
		}
		return asResult(out, anyFailed), out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "edit_video_metadata",
		Description: "Correct the snippet metadata (title, description, tags, category, default_language) of an EXISTING video on one of the pre-configured channels, via a YouTube Data API v3 read-modify-write. " +
			"This is a correction utility, NOT a views or discovery booster — editing metadata does not promote a video. " +
			"The caller chooses the channel, which must own the video; the tool never picks it. " +
			"Preserve-vs-clear: OMITTING a field preserves its current value; passing an empty value ('' or []) CLEARS it. title and category_id are required and cannot be cleared. " +
			"Costs ~51 quota units per edit (videos.list 1 + videos.update 50), separate from the ~100-uploads/day bucket. " +
			"On failure returns a structured error with the exact pipeline stage (validate, fetch_video, update_video) and a category (auth_error, quota_exceeded, invalid_request, network_error, policy_violation, other).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in EditVideoMetadataInput) (*mcp.CallToolResult, EditVideoMetadataOutput, error) {
		req := &uploader.EditRequest{
			Channel:         in.Channel,
			VideoID:         in.VideoID,
			Title:           in.Title,
			Description:     in.Description,
			Tags:            in.Tags,
			CategoryID:      in.CategoryID,
			DefaultLanguage: in.DefaultLanguage,
		}

		res, err := up.EditMetadata(ctx, req)
		if err != nil {
			out := editErrorOutput(in.Channel, in.VideoID, err)
			return asResult(out, true), out, nil
		}

		out := EditVideoMetadataOutput{
			Status:        "success",
			Channel:       res.Channel,
			VideoID:       res.VideoID,
			VideoURL:      res.VideoURL,
			UpdatedFields: res.UpdatedFields,
			Warnings:      res.Warnings,
		}
		return asResult(out, false), out, nil
	})
}

// editErrorOutput projects an edit failure onto EditVideoMetadataOutput,
// mirroring the upload_video error-mapping block: a *uploader.StageError
// surfaces its stage/category/error/hint; anything else falls back to CatOther.
func editErrorOutput(channel, videoID string, err error) EditVideoMetadataOutput {
	out := EditVideoMetadataOutput{Status: "error", Channel: channel, VideoID: videoID}
	var se *uploader.StageError
	if errors.As(err, &se) {
		out.Stage = se.Stage
		out.Category = string(se.Category)
		out.Error = se.Err.Error()
		out.Hint = se.Hint
	} else {
		out.Category = string(uploader.CatOther)
		out.Error = err.Error()
	}
	return out
}

// asResult renders v as pretty JSON text content; isError marks tool-level
// failure per the MCP spec.
func asResult(v any, isError bool) *mcp.CallToolResult {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		b = []byte(`{"status":"error","error":"failed to encode result"}`)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
		IsError: isError,
	}
}
