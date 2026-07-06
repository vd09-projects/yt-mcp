package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReadTokenFile(t *testing.T) {
	dir := t.TempDir()

	good := filepath.Join(dir, "good.token.json")
	if err := os.WriteFile(good, []byte(`{"refresh_token":"rt-123","channel_id":"UC-abc","scopes":["s1"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty.token.json")
	if err := os.WriteFile(empty, []byte(`{"channel_id":"UC-abc"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	malformed := filepath.Join(dir, "bad.token.json")
	if err := os.WriteFile(malformed, []byte(`{not json`), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr bool
		wantRT  string
	}{
		{name: "present", path: good, wantRT: "rt-123"},
		{name: "missing", path: filepath.Join(dir, "nope.json"), wantErr: true},
		{name: "malformed", path: malformed, wantErr: true},
		{name: "no refresh_token", path: empty, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tf, err := ReadTokenFile(tc.path)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tf.RefreshToken != tc.wantRT {
				t.Errorf("refresh_token = %q, want %q", tf.RefreshToken, tc.wantRT)
			}
		})
	}
}

func TestWriteTokenFile(t *testing.T) {
	dir := t.TempDir()
	// Nested path exercises MkdirAll.
	path := filepath.Join(dir, "nested", "main.token.json")

	tf := &TokenFile{RefreshToken: "rt-xyz", ChannelID: "UC-1", ObtainedAt: "2026-07-07T00:00:00Z", Scopes: []string{"s1", "s2"}}
	if err := WriteTokenFile(path, tf); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Round-trips — every persisted field, so a tag typo is caught.
	got, err := ReadTokenFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.RefreshToken != "rt-xyz" || got.ChannelID != "UC-1" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if got.ObtainedAt != "2026-07-07T00:00:00Z" {
		t.Errorf("ObtainedAt = %q, want round-trip", got.ObtainedAt)
	}
	if len(got.Scopes) != 2 || got.Scopes[0] != "s1" || got.Scopes[1] != "s2" {
		t.Errorf("Scopes = %v, want [s1 s2]", got.Scopes)
	}

	// 0600 perms (POSIX only).
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("perm = %o, want 600", perm)
		}
	}

	// No stray .tmp left behind (atomic rename cleaned up).
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file was not removed")
	}
}

func TestWriteTokenFileRejectsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.token.json")
	if err := WriteTokenFile(path, &TokenFile{}); err == nil {
		t.Fatal("expected error writing empty refresh_token")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file should not have been created")
	}
}
