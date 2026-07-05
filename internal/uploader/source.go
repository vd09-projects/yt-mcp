package uploader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// httpClient has no wall-clock timeout: large video downloads must not hit a
// fixed cap. Per-request lifetime is governed by the caller's context.
var httpClient = &http.Client{}

// resolveSource makes the media bytes available as a local file. Spec §4.2
// requires supporting both local disk paths and remote URLs (e.g.
// GitHub-hosted). It returns the local path, the content's SHA-256 (used for
// derived idempotency keys, spec §6, and cross-channel duplicate detection,
// spec §5.3), the size in bytes, and a cleanup func (a no-op for local
// files — the tool never deletes caller-owned files).
func resolveSource(ctx context.Context, source string) (path, sha string, size int64, cleanup func(), err error) {
	noop := func() {}
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		return downloadToTemp(ctx, source)
	}

	f, err := os.Open(source)
	if err != nil {
		return "", "", 0, noop, fmt.Errorf("open local file %s: %w", source, err)
	}
	defer f.Close()

	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", "", 0, noop, fmt.Errorf("hash local file %s: %w", source, err)
	}
	return source, hex.EncodeToString(h.Sum(nil)), n, noop, nil
}

func downloadToTemp(ctx context.Context, rawURL string) (string, string, int64, func(), error) {
	noop := func() {}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", "", 0, noop, fmt.Errorf("build request for %s: %w", rawURL, err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", 0, noop, fmt.Errorf("download %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", 0, noop, fmt.Errorf("download %s: unexpected HTTP status %s", rawURL, resp.Status)
	}

	tmp, err := os.CreateTemp("", "yt-upload-*.media")
	if err != nil {
		return "", "", 0, noop, fmt.Errorf("create temp file: %w", err)
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), resp.Body); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", "", 0, noop, fmt.Errorf("download %s: %w", rawURL, err)
	}
	size, err := tmp.Seek(0, io.SeekCurrent)
	if err != nil {
		size = 0
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", "", 0, noop, fmt.Errorf("finalize temp file: %w", err)
	}
	cleanup := func() { os.Remove(tmp.Name()) }
	return tmp.Name(), hex.EncodeToString(h.Sum(nil)), size, cleanup, nil
}
