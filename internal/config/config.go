// Package config loads the static setup configuration. Channel routing is
// deliberately static (spec §4.1): a setup-time config maps a channel alias
// to its credentials and per-channel defaults. No dynamic "which channel does
// this belong on" logic lives in the tool — the calling application decides
// that (spec §3).
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// OAuthClient holds the shared Google OAuth 2.0 client credentials.
// One Google Cloud project + one OAuth client serves all channels; each
// channel has its own refresh token (spec §5.1).
type OAuthClient struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// Channel is one pre-configured YouTube channel (spec §4.1 expects 2-3).
type Channel struct {
	// TokenFile is the path to this channel's token file (see TokenFile),
	// written by yt-authorize and read at Load. The path is ${ENV_VAR}-
	// expandable like the rest of the config, so deployments can point it at
	// a mounted secret without editing the config body.
	TokenFile string `json:"token_file"`

	// RefreshTokenLegacy binds the removed `refresh_token` field purely so
	// Load can emit a migration error instead of silently ignoring an old
	// config. See validate.
	RefreshTokenLegacy string `json:"refresh_token,omitempty"`

	// RefreshToken is populated at Load from TokenFile — it is NOT read from
	// the config JSON. Downstream (uploader) reads it from here unchanged.
	RefreshToken string `json:"-"`
	// ChannelID is populated at Load from TokenFile (may be empty on older
	// token files); lets verify_channels detect a wrong-account token.
	ChannelID string `json:"-"`

	DefaultCategoryID string   `json:"default_category_id,omitempty"`
	DefaultPrivacy    string   `json:"default_privacy,omitempty"`
	DefaultTags       []string `json:"default_tags,omitempty"`
	Description       string   `json:"description,omitempty"`
}

// Config is the root configuration document.
type Config struct {
	OAuth    OAuthClient         `json:"oauth"`
	StateDir string              `json:"state_dir,omitempty"`
	Channels map[string]*Channel `json:"channels"`

	// BlockCrossChannelDuplicates controls the spec §5.3 guard: when true,
	// an upload whose content hash was already successfully published to a
	// DIFFERENT channel is rejected unless the caller explicitly passes
	// allow_cross_channel_duplicate. When false, the upload proceeds but the
	// response carries a warning.
	BlockCrossChannelDuplicates bool `json:"block_cross_channel_duplicates,omitempty"`

	// MinSecondsBetweenUploads, when > 0, rejects uploads fired closer
	// together than this across ALL channels (timing-clustering guard,
	// spec §5.3). 0 disables the guard.
	MinSecondsBetweenUploads int `json:"min_seconds_between_uploads,omitempty"`

	// UploadChunkSizeMB is the resumable-upload chunk size. Defaults to 8.
	UploadChunkSizeMB int `json:"upload_chunk_size_mb,omitempty"`
}

// Load reads the JSON config at path, expanding ${ENV_VAR} references so
// secrets (client secret, refresh tokens) can live in the environment
// instead of on disk.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	expanded := os.Expand(string(raw), os.Getenv)

	var cfg Config
	if err := json.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	if cfg.StateDir == "" {
		cfg.StateDir = "state"
	}
	if abs, err := filepath.Abs(cfg.StateDir); err == nil {
		cfg.StateDir = abs
	}
	if cfg.UploadChunkSizeMB <= 0 {
		cfg.UploadChunkSizeMB = 8
	}

	// Resolve each channel's refresh token from its token file. Done after
	// validate so a structurally-bad config fails before we touch the disk.
	for alias, ch := range cfg.Channels {
		tf, err := ReadTokenFile(ch.TokenFile)
		if err != nil {
			return nil, fmt.Errorf("config: channel %q: %w", alias, err)
		}
		ch.RefreshToken = tf.RefreshToken
		ch.ChannelID = tf.ChannelID
	}
	return &cfg, nil
}

func (c *Config) validate() error {
	if c.OAuth.ClientID == "" || c.OAuth.ClientSecret == "" {
		return fmt.Errorf("config: oauth.client_id and oauth.client_secret are required (set the env vars referenced in the config)")
	}
	if len(c.Channels) == 0 {
		return fmt.Errorf("config: at least one channel must be configured")
	}
	for alias, ch := range c.Channels {
		if ch == nil {
			return fmt.Errorf("config: channel %q is null", alias)
		}
		if ch.RefreshTokenLegacy != "" {
			return fmt.Errorf("config: channel %q uses the removed `refresh_token` field; replace it with `token_file` (path to the JSON written by yt-authorize) — see config.example.json", alias)
		}
		if ch.TokenFile == "" {
			return fmt.Errorf("config: channel %q is missing token_file (run yt-authorize --channel %s to create it)", alias, alias)
		}
		if ch.DefaultPrivacy != "" && !ValidPrivacy(ch.DefaultPrivacy) {
			return fmt.Errorf("config: channel %q has invalid default_privacy %q (must be public, unlisted or private)", alias, ch.DefaultPrivacy)
		}
		if ch.DefaultCategoryID != "" {
			if _, err := strconv.Atoi(ch.DefaultCategoryID); err != nil {
				return fmt.Errorf("config: channel %q default_category_id must be a numeric YouTube category id", alias)
			}
		}
	}
	return nil
}

// LoadOAuthOnly reads just the oauth client block, skipping channel
// validation. Used by yt-authorize, which necessarily runs BEFORE any
// per-channel refresh tokens exist in the config.
func LoadOAuthOnly(path string) (*OAuthClient, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	expanded := os.Expand(string(raw), os.Getenv)

	var cfg struct {
		OAuth OAuthClient `json:"oauth"`
	}
	if err := json.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if cfg.OAuth.ClientID == "" || cfg.OAuth.ClientSecret == "" {
		return nil, fmt.Errorf("config: oauth.client_id and oauth.client_secret are required (set the env vars referenced in the config)")
	}
	return &cfg.OAuth, nil
}

// ResolveTokenFilePath returns the token_file path configured for a channel
// alias, expanding ${ENV_VAR} references. Used by yt-authorize, which must know
// WHERE to write a channel's token before that token (and thus a fully valid
// config) exists — so it deliberately skips channel token validation.
func ResolveTokenFilePath(path, alias string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read config: %w", err)
	}
	expanded := os.Expand(string(raw), os.Getenv)

	var cfg struct {
		Channels map[string]*Channel `json:"channels"`
	}
	if err := json.Unmarshal([]byte(expanded), &cfg); err != nil {
		return "", fmt.Errorf("parse config %s: %w", path, err)
	}
	ch, ok := cfg.Channels[alias]
	if !ok {
		return "", fmt.Errorf("config: no channel %q (known channels come from config.json)", alias)
	}
	if ch.TokenFile == "" {
		return "", fmt.Errorf("config: channel %q has no token_file path", alias)
	}
	return ch.TokenFile, nil
}

// ValidPrivacy reports whether p is a privacy status the YouTube API accepts.
func ValidPrivacy(p string) bool {
	switch p {
	case "public", "unlisted", "private":
		return true
	}
	return false
}
