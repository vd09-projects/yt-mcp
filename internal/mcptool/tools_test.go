package mcptool

import (
	"errors"
	"testing"

	"yt-mcp/internal/uploader"
)

// TestEditErrorOutput pins the edit_video_metadata failure projection: a
// *uploader.StageError surfaces its stage/category/error/hint; a plain error
// falls back to CatOther. This mirrors the upload_video error-mapping block.
func TestEditErrorOutput(t *testing.T) {
	t.Run("stage_error_projected", func(t *testing.T) {
		se := &uploader.StageError{
			Stage:    "fetch_video",
			Category: uploader.CatInvalid,
			Hint:     "check the id",
			Err:      errors.New("no such video"),
		}
		out := editErrorOutput("main", "vid1", se)
		if out.Status != "error" {
			t.Errorf("status = %q, want error", out.Status)
		}
		if out.Channel != "main" || out.VideoID != "vid1" {
			t.Errorf("channel/video not carried: %+v", out)
		}
		if out.Stage != "fetch_video" {
			t.Errorf("stage = %q", out.Stage)
		}
		if out.Category != string(uploader.CatInvalid) {
			t.Errorf("category = %q", out.Category)
		}
		if out.Error != "no such video" {
			t.Errorf("error = %q", out.Error)
		}
		if out.Hint != "check the id" {
			t.Errorf("hint = %q", out.Hint)
		}
	})

	t.Run("plain_error_falls_back_to_other", func(t *testing.T) {
		out := editErrorOutput("main", "vid2", errors.New("boom"))
		if out.Category != string(uploader.CatOther) {
			t.Errorf("category = %q, want %q", out.Category, uploader.CatOther)
		}
		if out.Error != "boom" {
			t.Errorf("error = %q", out.Error)
		}
		if out.Stage != "" {
			t.Errorf("stage should be empty for non-StageError, got %q", out.Stage)
		}
	})
}
