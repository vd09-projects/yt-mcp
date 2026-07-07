package uploader

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"golang.org/x/oauth2"
	"google.golang.org/api/googleapi"
)

// Category is the fixed failure taxonomy from spec §7, designed so
// root-causing from logs is fast for a human or an AI.
type Category string

const (
	CatAuth    Category = "auth_error"
	CatQuota   Category = "quota_exceeded"
	CatInvalid Category = "invalid_request"
	CatNetwork Category = "network_error"
	CatPolicy  Category = "policy_violation" // local guards: §5.3 duplicates/timing, in-flight key
	CatOther   Category = "other"
)

// StageError is the structured failure demanded by spec §7: it pins the
// exact pipeline stage, a machine-readable category, and — when we can tell —
// a hint for the fix. It also carries partial-failure/rollback context.
type StageError struct {
	Stage    string
	Category Category
	Hint     string
	Err      error

	// RolledBack is true when a partially-completed upload was successfully
	// deleted (spec §7 rollback). OrphanedVideoID is set when rollback was
	// attempted but the delete itself failed, leaving a live video behind.
	RolledBack      bool
	OrphanedVideoID string
	RollbackErr     error
}

func (e *StageError) Error() string {
	msg := fmt.Sprintf("stage=%s category=%s: %v", e.Stage, e.Category, e.Err)
	if e.RolledBack {
		msg += " (the uploaded video was rolled back / deleted)"
	}
	if e.OrphanedVideoID != "" {
		msg += fmt.Sprintf(" (rollback FAILED, orphaned video id=%s: %v)", e.OrphanedVideoID, e.RollbackErr)
	}
	return msg
}

func (e *StageError) Unwrap() error { return e.Err }

// stageErr wraps err with its pipeline stage and auto-derived category.
func stageErr(stage string, err error) *StageError {
	cat, hint := Categorize(err)
	return &StageError{Stage: stage, Category: cat, Hint: hint, Err: err}
}

// invalid builds a CatInvalid StageError from a format string.
func invalid(stage, format string, args ...any) *StageError {
	return &StageError{Stage: stage, Category: CatInvalid, Err: fmt.Errorf(format, args...)}
}

// Categorize maps an arbitrary error onto the §7 taxonomy and attaches an
// actionable hint where the failure mode is a known one.
func Categorize(err error) (Category, string) {
	// OAuth token refresh failures surface before any YouTube API call.
	var rerr *oauth2.RetrieveError
	if errors.As(err, &rerr) {
		if rerr.ErrorCode == "invalid_grant" {
			return CatAuth, "refresh token rejected (invalid_grant). If the OAuth consent screen is still in \"Testing\" publishing status, refresh tokens expire every 7 days (spec §5.1) — re-run yt-authorize for this channel, or complete Google app verification for long-lived tokens"
		}
		return CatAuth, "OAuth token refresh failed; check the client credentials and the channel's refresh token"
	}

	var gerr *googleapi.Error
	if errors.As(err, &gerr) {
		switch {
		case gerr.Code == 401:
			return CatAuth, "credentials rejected by the YouTube API; re-run yt-authorize for this channel"
		case gerr.Code == 403:
			for _, item := range gerr.Errors {
				switch item.Reason {
				case "quotaExceeded", "dailyLimitExceeded", "rateLimitExceeded",
					"userRateLimitExceeded", "uploadLimitExceeded":
					return CatQuota, "YouTube Data API quota or upload limit hit; videos.insert costs 1 unit but is separately capped at ~100 uploads/day (spec §5.2)"
				}
			}
			return CatAuth, "the authorized account is not permitted to perform this action on the target channel"
		case gerr.Code == 400:
			return CatInvalid, ""
		case gerr.Code == 404:
			return CatInvalid, "a referenced resource was not found (check playlist_id / video id)"
		case gerr.Code >= 500:
			return CatNetwork, "YouTube API server-side error; safe to retry with the SAME idempotency key"
		}
		return CatOther, ""
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return CatNetwork, "the request timed out or was canceled; retry with the SAME idempotency key so a slow-but-successful publish is not duplicated (spec §6)"
	}
	var nerr net.Error
	if errors.As(err, &nerr) {
		return CatNetwork, "network failure; retry with the SAME idempotency key"
	}
	var uerr *url.Error
	if errors.As(err, &uerr) {
		return CatNetwork, "network failure while talking to a remote host"
	}
	if strings.Contains(strings.ToLower(err.Error()), "connection") {
		return CatNetwork, ""
	}
	return CatOther, ""
}
