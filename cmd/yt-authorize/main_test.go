package main

import (
	"testing"

	"yt-mcp/internal/config"
)

// TestResolveCreds pins the credential precedence and — critically — the
// argument order at the call site (flagID first, flagSecret second): a
// transposition would let the wrong value flow into ClientID/ClientSecret.
func TestResolveCreds(t *testing.T) {
	tests := []struct {
		name       string
		flagID     string
		flagSecret string
		oc         *config.OAuthClient
		env        map[string]string
		wantID     string
		wantSecret string
	}{
		{
			name:       "flags win over config and env",
			flagID:     "flag-id",
			flagSecret: "flag-secret",
			oc:         &config.OAuthClient{ClientID: "cfg-id", ClientSecret: "cfg-secret"},
			env:        map[string]string{"YT_CLIENT_ID": "env-id", "YT_CLIENT_SECRET": "env-secret"},
			wantID:     "flag-id",
			wantSecret: "flag-secret",
		},
		{
			name:       "config wins over env when no flags",
			oc:         &config.OAuthClient{ClientID: "cfg-id", ClientSecret: "cfg-secret"},
			env:        map[string]string{"YT_CLIENT_ID": "env-id", "YT_CLIENT_SECRET": "env-secret"},
			wantID:     "cfg-id",
			wantSecret: "cfg-secret",
		},
		{
			name:       "env used when no flags and no config",
			env:        map[string]string{"YT_CLIENT_ID": "env-id", "YT_CLIENT_SECRET": "env-secret"},
			wantID:     "env-id",
			wantSecret: "env-secret",
		},
		{
			name:       "id and secret resolve independently",
			flagID:     "flag-id",
			oc:         &config.OAuthClient{ClientSecret: "cfg-secret"},
			wantID:     "flag-id",
			wantSecret: "cfg-secret",
		},
		{
			name:       "arg order — id maps to id, secret to secret",
			flagID:     "the-id",
			flagSecret: "the-secret",
			wantID:     "the-id",
			wantSecret: "the-secret",
		},
		{
			name:       "all empty yields empty",
			wantID:     "",
			wantSecret: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			gotID, gotSecret := resolveCreds(tt.flagID, tt.flagSecret, tt.oc)
			if gotID != tt.wantID {
				t.Errorf("id = %q, want %q", gotID, tt.wantID)
			}
			if gotSecret != tt.wantSecret {
				t.Errorf("secret = %q, want %q", gotSecret, tt.wantSecret)
			}
		})
	}
}
