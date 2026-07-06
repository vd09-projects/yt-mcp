package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile is a tiny helper for building fixture files in a temp dir.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadResolvesTokenFile(t *testing.T) {
	dir := t.TempDir()
	tokPath := filepath.Join(dir, "main.token.json")
	writeFile(t, tokPath, `{"refresh_token":"rt-main","channel_id":"UC-main"}`)

	cfgPath := filepath.Join(dir, "config.json")
	writeFile(t, cfgPath, `{
      "oauth": {"client_id": "cid", "client_secret": "csec"},
      "state_dir": "`+dir+`",
      "channels": {
        "main": {"token_file": "`+tokPath+`", "default_privacy": "unlisted"}
      }
    }`)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	ch := cfg.Channels["main"]
	if ch.RefreshToken != "rt-main" {
		t.Errorf("RefreshToken = %q, want rt-main", ch.RefreshToken)
	}
	if ch.ChannelID != "UC-main" {
		t.Errorf("ChannelID = %q, want UC-main", ch.ChannelID)
	}
}

func TestLoadRejectsLegacyRefreshToken(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	writeFile(t, cfgPath, `{
      "oauth": {"client_id": "cid", "client_secret": "csec"},
      "channels": {"main": {"refresh_token": "rt-legacy"}}
    }`)

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for legacy refresh_token field")
	}
	if !strings.Contains(err.Error(), "token_file") {
		t.Errorf("error should mention token_file migration, got: %v", err)
	}
}

func TestLoadMissingTokenFileField(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	writeFile(t, cfgPath, `{
      "oauth": {"client_id": "cid", "client_secret": "csec"},
      "channels": {"main": {"default_privacy": "unlisted"}}
    }`)

	_, err := Load(cfgPath)
	if err == nil || !strings.Contains(err.Error(), "token_file") {
		t.Fatalf("expected missing token_file error, got: %v", err)
	}
}

func TestLoadMissingTokenFileOnDisk(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	writeFile(t, cfgPath, `{
      "oauth": {"client_id": "cid", "client_secret": "csec"},
      "channels": {"main": {"token_file": "`+filepath.Join(dir, "absent.json")+`"}}
    }`)

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for absent token file")
	}
	if !strings.Contains(err.Error(), "yt-authorize") {
		t.Errorf("error should hint to run yt-authorize, got: %v", err)
	}
}

func TestResolveTokenFilePath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("YT_TOKEN_FILE_MAIN", "/secrets/main.json")
	cfgPath := filepath.Join(dir, "config.json")
	writeFile(t, cfgPath, `{
      "oauth": {"client_id": "cid", "client_secret": "csec"},
      "channels": {
        "main": {"token_file": "${YT_TOKEN_FILE_MAIN}"},
        "shorts": {"token_file": "state/shorts.token.json"}
      }
    }`)

	got, err := ResolveTokenFilePath(cfgPath, "main")
	if err != nil {
		t.Fatalf("resolve main: %v", err)
	}
	if got != "/secrets/main.json" {
		t.Errorf("main path = %q, want /secrets/main.json (env-expanded)", got)
	}

	got, err = ResolveTokenFilePath(cfgPath, "shorts")
	if err != nil {
		t.Fatalf("resolve shorts: %v", err)
	}
	if got != "state/shorts.token.json" {
		t.Errorf("shorts path = %q", got)
	}

	if _, err := ResolveTokenFilePath(cfgPath, "ghost"); err == nil {
		t.Error("expected error for unknown channel")
	}
}
