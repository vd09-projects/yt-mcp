package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// TokenFile is the on-disk per-channel credential written by yt-authorize and
// read by yt-upload-mcp. The env var / config `token_file` field holds the
// PATH to this file (GOOGLE_APPLICATION_CREDENTIALS-style) — the secret itself
// never lives in an env value or in a git-tracked path.
type TokenFile struct {
	// RefreshToken is the long-lived per-channel OAuth refresh token.
	RefreshToken string `json:"refresh_token"`
	// ChannelID is the YouTube channel the token actually controls (UC...),
	// captured at authorize time so verify_channels can detect a token that
	// was minted under the wrong brand account.
	ChannelID string `json:"channel_id,omitempty"`
	// ChannelTitle is a human label for the same, purely informational.
	ChannelTitle string `json:"channel_title,omitempty"`
	// ObtainedAt is when the grant was made (RFC3339). Lets callers warn
	// before the ~7-day Testing-mode expiry.
	ObtainedAt string `json:"obtained_at,omitempty"`
	// Scopes are the OAuth scopes the token was granted.
	Scopes []string `json:"scopes,omitempty"`
}

// ReadTokenFile loads and validates a token file at path. A missing or
// malformed file is a distinct, actionable error (the caller should re-run
// yt-authorize), not a generic parse failure.
func ReadTokenFile(path string) (*TokenFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("token file %s does not exist: run yt-authorize for this channel: %w", path, err)
		}
		return nil, fmt.Errorf("read token file %s: %w", path, err)
	}
	var tf TokenFile
	if err := json.Unmarshal(raw, &tf); err != nil {
		return nil, fmt.Errorf("parse token file %s (re-run yt-authorize to regenerate it): %w", path, err)
	}
	if tf.RefreshToken == "" {
		return nil, fmt.Errorf("token file %s has no refresh_token: re-run yt-authorize", path)
	}
	return &tf, nil
}

// WriteTokenFile writes tf to path atomically (temp file + rename) with 0600
// permissions, creating parent directories as needed. Mirrors the store's
// ledger-persist convention (temp + rename, 0o600).
func WriteTokenFile(path string, tf *TokenFile) error {
	if tf == nil || tf.RefreshToken == "" {
		return fmt.Errorf("refuse to write token file %s with an empty refresh_token", path)
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create token dir %s: %w", dir, err)
		}
	}
	b, err := json.MarshalIndent(tf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal token file: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write temp token file %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("finalize token file %s: %w", path, err)
	}
	return nil
}

// NowRFC3339 returns the current time formatted for a token file's
// obtained_at field. Isolated so tests can supply a fixed clock.
func NowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
