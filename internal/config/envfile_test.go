package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeEnvFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	return path
}

// TestLoadEnvFile_FileWins is the load-bearing behaviour: a value in the env
// file must override a variable already set in the shell environment
// (godotenv.Overload, not Load).
func TestLoadEnvFile_FileWins(t *testing.T) {
	t.Setenv("YT_CLIENT_ID", "from-shell")
	path := writeEnvFile(t, "YT_CLIENT_ID=from-file\n")

	if err := LoadEnvFile(path); err != nil {
		t.Fatalf("LoadEnvFile: %v", err)
	}
	if got := os.Getenv("YT_CLIENT_ID"); got != "from-file" {
		t.Errorf("YT_CLIENT_ID = %q, want %q (file must win over shell)", got, "from-file")
	}
}

// TestLoadEnvFile_SetsUnset confirms plain loading of a var the shell did not set.
func TestLoadEnvFile_SetsUnset(t *testing.T) {
	t.Setenv("YT_REFRESH_TOKEN_SHORTS", "") // ensure clean slate under t.Setenv cleanup
	os.Unsetenv("YT_REFRESH_TOKEN_SHORTS")
	path := writeEnvFile(t, "YT_REFRESH_TOKEN_SHORTS=tok-123\n")

	if err := LoadEnvFile(path); err != nil {
		t.Fatalf("LoadEnvFile: %v", err)
	}
	if got := os.Getenv("YT_REFRESH_TOKEN_SHORTS"); got != "tok-123" {
		t.Errorf("YT_REFRESH_TOKEN_SHORTS = %q, want %q", got, "tok-123")
	}
}

func TestLoadEnvFile_MissingFile(t *testing.T) {
	err := LoadEnvFile(filepath.Join(t.TempDir(), "does-not-exist.env"))
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "does-not-exist.env") {
		t.Errorf("error should name the path, got: %v", err)
	}
}

// TestLoadEnvFile_MalformedNoLeak verifies a parse failure never surfaces the
// file's (secret-bearing) content in the returned error — only a generic
// message and the path.
func TestLoadEnvFile_MalformedNoLeak(t *testing.T) {
	const secret = "SUPERSECRETTOKENVALUE"
	// A bare line with no '=' is malformed for godotenv.
	path := writeEnvFile(t, secret+"\n")

	err := LoadEnvFile(path)
	if err == nil {
		t.Fatal("expected error for malformed file, got nil")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("error leaked file content: %v", err)
	}
}
