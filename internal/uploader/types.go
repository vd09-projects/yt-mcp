package uploader

// Request is one fully caller-specified upload. Per spec §3, the calling
// application decides WHAT to upload, WHERE (which channel alias), and with
// what metadata; this tool only knows HOW to execute it.
type Request struct {
	Channel     string
	Source      string // local path or http(s) URL (spec §4.2)
	Title       string
	Description string
	Tags        []string // backend keywords, distinct from description hashtags
	CategoryID  string
	// PrivacyStatus defaults to "unlisted" when neither the caller nor the
	// channel config specifies one — never silently public (spec §4.2).
	PrivacyStatus string
	// MadeForKids is a pointer so "not provided" is distinguishable from an
	// explicit false; the API requires the declaration (COPPA, spec §4.2).
	MadeForKids *bool
	Thumbnail   string
	PlaylistID  string
	PublishAt   string // RFC3339; requires privacy=private (spec §4.2)
	// IsShort is an explicit caller flag (spec §4.3): there is no Shorts
	// endpoint, so the tool never infers — it just reinforces with #Shorts.
	IsShort        bool
	IdempotencyKey string

	// RollbackOnPartialFailure defaults to true (spec §7): delete the video
	// if a follow-up step (thumbnail/playlist) fails after upload succeeded.
	RollbackOnPartialFailure *bool
	// AllowCrossChannelDuplicate overrides the §5.3 duplicate-content block
	// when the config enables it.
	AllowCrossChannelDuplicate bool
}

// Result is the successful (or deduplicated) outcome of an upload.
type Result struct {
	Channel         string
	VideoID         string
	VideoURL        string
	ShortsURL       string
	IdempotencyKey  string
	Deduplicated    bool
	PrivacyStatus   string
	PublishAt       string
	ThumbnailSet    bool
	AddedToPlaylist bool
	Warnings        []string
}
