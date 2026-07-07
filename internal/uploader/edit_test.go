package uploader

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"google.golang.org/api/googleapi"
	"google.golang.org/api/youtube/v3"

	"yt-mcp/internal/config"
)

func strPtr(s string) *string       { return &s }
func slicePtr(s []string) *[]string { return &s }

// baseSnippet is a representative fetched snippet, including the read-only
// members the API populates but ignores on write.
func baseSnippet() *youtube.VideoSnippet {
	return &youtube.VideoSnippet{
		Title:                "Original Title",
		Description:          "Original description.",
		Tags:                 []string{"alpha", "beta"},
		CategoryId:           "22",
		DefaultLanguage:      "en",
		ChannelId:            "UC_channel",
		ChannelTitle:         "Channel Name",
		PublishedAt:          "2020-01-01T00:00:00Z",
		LiveBroadcastContent: "none",
		Thumbnails:           &youtube.ThumbnailDetails{Default: &youtube.Thumbnail{Url: "http://x/t.jpg"}},
		Localized:            &youtube.VideoLocalization{Title: "loc", Description: "locd"},
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// ---- Group 1: mergeSnippet — preserve / overwrite / clear / reject / validate

func TestMergeSnippet(t *testing.T) {
	longTitle := strings.Repeat("x", 101)                 // 101 runes > 100
	overBudgetTags := []string{strings.Repeat("z", 501)}  // 501 runes > 500 budget
	byteHeavyRuneOK := []string{strings.Repeat("あ", 400)} // 1200 bytes but 400 runes <= 500

	cases := []struct {
		name          string
		edit          *EditRequest
		wantErrStage  string   // "" => no error
		wantForceSend []string // sorted set expected in merged.ForceSendFields
		check         func(t *testing.T, m *youtube.VideoSnippet)
	}{
		{
			name: "preserve_all_nil_pointers",
			edit: &EditRequest{},
			check: func(t *testing.T, m *youtube.VideoSnippet) {
				if m.Title != "Original Title" || m.Description != "Original description." ||
					m.CategoryId != "22" || m.DefaultLanguage != "en" {
					t.Errorf("preserve changed a field: %+v", m)
				}
				if !equalStrings(m.Tags, []string{"alpha", "beta"}) {
					t.Errorf("tags not preserved: %v", m.Tags)
				}
			},
		},
		{
			name: "overwrite_title_and_tags",
			edit: &EditRequest{Title: strPtr("New Title"), Tags: slicePtr([]string{"one"})},
			check: func(t *testing.T, m *youtube.VideoSnippet) {
				if m.Title != "New Title" {
					t.Errorf("title = %q", m.Title)
				}
				if !equalStrings(m.Tags, []string{"one"}) {
					t.Errorf("tags = %v", m.Tags)
				}
				if m.Description != "Original description." {
					t.Errorf("description should be preserved, got %q", m.Description)
				}
			},
		},
		{
			name:          "clear_optional_description_tags_language",
			edit:          &EditRequest{Description: strPtr(""), Tags: slicePtr([]string{}), DefaultLanguage: strPtr("")},
			wantForceSend: []string{"DefaultLanguage", "Description", "Tags"},
			check: func(t *testing.T, m *youtube.VideoSnippet) {
				if m.Description != "" || m.DefaultLanguage != "" || len(m.Tags) != 0 {
					t.Errorf("clears not applied: %+v", m)
				}
			},
		},
		{
			name:         "clear_required_title_rejected",
			edit:         &EditRequest{Title: strPtr("")},
			wantErrStage: stageValidate,
		},
		{
			name:         "clear_required_category_rejected",
			edit:         &EditRequest{CategoryID: strPtr("   ")},
			wantErrStage: stageValidate,
		},
		{
			name:         "validate_title_too_long",
			edit:         &EditRequest{Title: strPtr(longTitle)},
			wantErrStage: stageValidate,
		},
		{
			name:         "validate_title_angle_brackets",
			edit:         &EditRequest{Title: strPtr("bad <title>")},
			wantErrStage: stageValidate,
		},
		{
			name:         "validate_category_non_numeric",
			edit:         &EditRequest{CategoryID: strPtr("Music")},
			wantErrStage: stageValidate,
		},
		{
			name:         "validate_tags_over_budget",
			edit:         &EditRequest{Tags: slicePtr(overBudgetTags)},
			wantErrStage: stageValidate,
		},
		{
			name: "rune_vs_byte_tags_accepted",
			edit: &EditRequest{Tags: slicePtr(byteHeavyRuneOK)},
			check: func(t *testing.T, m *youtube.VideoSnippet) {
				if !equalStrings(m.Tags, byteHeavyRuneOK) {
					t.Errorf("multibyte tags not applied")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cur := baseSnippet()
			m, _, se := mergeSnippet(cur, tc.edit)
			if tc.wantErrStage != "" {
				if se == nil {
					t.Fatalf("want error at stage %q, got nil", tc.wantErrStage)
				}
				if se.Stage != tc.wantErrStage {
					t.Errorf("stage = %q, want %q", se.Stage, tc.wantErrStage)
				}
				if se.Category != CatInvalid {
					t.Errorf("category = %q, want %q", se.Category, CatInvalid)
				}
				return
			}
			if se != nil {
				t.Fatalf("unexpected error: %v", se)
			}
			// mergeSnippet must never mutate current.
			if cur.Title != "Original Title" || !equalStrings(cur.Tags, []string{"alpha", "beta"}) {
				t.Errorf("mergeSnippet mutated current: %+v", cur)
			}
			if tc.wantForceSend != nil {
				got := append([]string(nil), m.ForceSendFields...)
				sort.Strings(got)
				if !equalStrings(got, tc.wantForceSend) {
					t.Errorf("ForceSendFields = %v, want %v", got, tc.wantForceSend)
				}
			}
			if tc.check != nil {
				tc.check(t, m)
			}
		})
	}
}

// ---- Group 2: wire seam — ForceSendFields reaches Update (BLOCKING-1) --------

// fakeVideoService returns a canned snippet from List and captures the outgoing
// *youtube.Video handed to Update, so a test can inspect exactly what would go
// on the wire.
type fakeVideoService struct {
	listResp   *youtube.VideoListResponse
	listErr    error
	updateErr  error
	captured   *youtube.Video
	listCalls  int
	updateCall int
}

func (f *fakeVideoService) List(_ context.Context, _ string) (*youtube.VideoListResponse, error) {
	f.listCalls++
	return f.listResp, f.listErr
}

func (f *fakeVideoService) Update(_ context.Context, v *youtube.Video) (*youtube.Video, error) {
	f.updateCall++
	f.captured = v
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	return v, nil
}

func TestEditMetadata_WireSeam_ForceSendFields(t *testing.T) {
	fake := &fakeVideoService{
		listResp: &youtube.VideoListResponse{Items: []*youtube.Video{{Snippet: baseSnippet()}}},
	}
	req := &EditRequest{
		Channel:     "test",
		VideoID:     "vid123",
		Title:       strPtr("Corrected Title"),
		Description: strPtr(""),           // clear
		Tags:        slicePtr([]string{}), // clear
	}

	res, err := editMetadata(context.Background(), fake, req)
	if err != nil {
		t.Fatalf("editMetadata: %v", err)
	}
	if fake.captured == nil {
		t.Fatal("Update was never called")
	}
	sn := fake.captured.Snippet
	got := append([]string(nil), sn.ForceSendFields...)
	sort.Strings(got)
	if !equalStrings(got, []string{"Description", "Tags"}) {
		t.Errorf("wire ForceSendFields = %v, want [Description Tags]", got)
	}
	if sn.Title != "Corrected Title" {
		t.Errorf("wire Title = %q, want Corrected Title", sn.Title)
	}
	if sn.CategoryId != "22" || sn.DefaultLanguage != "en" {
		t.Errorf("untouched fields lost their current values: cat=%q lang=%q", sn.CategoryId, sn.DefaultLanguage)
	}
	if fake.captured.Id != "vid123" {
		t.Errorf("wire video id = %q, want vid123", fake.captured.Id)
	}
	if res.VideoURL != "https://www.youtube.com/watch?v=vid123" {
		t.Errorf("VideoURL = %q", res.VideoURL)
	}
}

// ---- Group 3: UpdatedFields semantics (BLOCKING-2) --------------------------

func TestMergeSnippet_UpdatedFields(t *testing.T) {
	cases := []struct {
		name         string
		edit         *EditRequest
		wantUpdated  []string
		wantForce    []string // subset assertions for the clear cases
		forceMustNot []string
	}{
		{
			name:        "overwrite_new_value_listed",
			edit:        &EditRequest{Title: strPtr("Different")},
			wantUpdated: []string{"title"},
		},
		{
			name:        "overwrite_same_value_not_listed",
			edit:        &EditRequest{Title: strPtr("Original Title")},
			wantUpdated: nil,
		},
		{
			name:        "clear_nonempty_description_listed",
			edit:        &EditRequest{Description: strPtr("")},
			wantUpdated: []string{"description"},
			wantForce:   []string{"Description"},
		},
		{
			name:         "clear_already_empty_language_not_listed_but_forced",
			edit:         &EditRequest{DefaultLanguage: strPtr("")},
			wantUpdated:  nil, // current DefaultLanguage set below to "" so no diff
			wantForce:    []string{"DefaultLanguage"},
			forceMustNot: nil,
		},
		{
			name:        "preserve_not_listed",
			edit:        &EditRequest{},
			wantUpdated: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cur := baseSnippet()
			if tc.name == "clear_already_empty_language_not_listed_but_forced" {
				cur.DefaultLanguage = "" // already empty, so clearing is a no-op diff
			}
			m, updated, se := mergeSnippet(cur, tc.edit)
			if se != nil {
				t.Fatalf("unexpected error: %v", se)
			}
			sort.Strings(updated)
			want := append([]string(nil), tc.wantUpdated...)
			sort.Strings(want)
			if !equalStrings(updated, want) {
				t.Errorf("UpdatedFields = %v, want %v", updated, want)
			}
			for _, f := range tc.wantForce {
				if !contains(m.ForceSendFields, f) {
					t.Errorf("ForceSendFields %v missing %q — clears must be force-sent even when not in UpdatedFields", m.ForceSendFields, f)
				}
			}
		})
	}
}

// ---- Group 4: read-only fields ride along + localized reconciliation --------

func TestMergeSnippet_ReadOnlyFieldsAndLocalized(t *testing.T) {
	cur := baseSnippet()
	m, updated, se := mergeSnippet(cur, &EditRequest{
		Title:           strPtr("New"),
		DefaultLanguage: strPtr("fr"),
	})
	if se != nil {
		t.Fatalf("unexpected error: %v", se)
	}
	if m.ChannelId != cur.ChannelId || m.ChannelTitle != cur.ChannelTitle ||
		m.PublishedAt != cur.PublishedAt || m.LiveBroadcastContent != cur.LiveBroadcastContent ||
		m.Thumbnails != cur.Thumbnails {
		t.Errorf("read-only field not round-tripped: %+v", m)
	}
	for _, ro := range []string{"ChannelId", "ChannelTitle", "Thumbnails", "PublishedAt", "LiveBroadcastContent"} {
		if contains(m.ForceSendFields, ro) {
			t.Errorf("read-only field %q must not be force-sent", ro)
		}
		if contains(updated, strings.ToLower(ro)) {
			t.Errorf("read-only field %q must not be in UpdatedFields", ro)
		}
	}
	// Localized must be nil so the server reconciles it.
	if m.Localized != nil {
		t.Errorf("merged.Localized = %+v, want nil", m.Localized)
	}
	// The defaultLanguage overwrite still lands and is reported.
	if m.DefaultLanguage != "fr" {
		t.Errorf("DefaultLanguage = %q, want fr", m.DefaultLanguage)
	}
	if !contains(updated, "defaultLanguage") {
		t.Errorf("UpdatedFields %v missing defaultLanguage", updated)
	}
}

// ---- Group 5: taxonomy / stage mapping via synthetic googleapi errors -------

func TestEditMetadata_TaxonomyMapping(t *testing.T) {
	okList := func() *youtube.VideoListResponse {
		return &youtube.VideoListResponse{Items: []*youtube.Video{{Snippet: baseSnippet()}}}
	}
	quota403 := &googleapi.Error{Code: 403, Errors: []googleapi.ErrorItem{{Reason: "quotaExceeded"}}}
	other403 := &googleapi.Error{Code: 403, Errors: []googleapi.ErrorItem{{Reason: "forbidden"}}}

	cases := []struct {
		name      string
		fake      *fakeVideoService
		wantStage string
		wantCat   Category
		wantHint  string // substring, "" => skip
	}{
		{
			name:      "empty_items_fetch_invalid",
			fake:      &fakeVideoService{listResp: &youtube.VideoListResponse{Items: nil}},
			wantStage: stageFetchVideo,
			wantCat:   CatInvalid,
			wantHint:  "may not belong to",
		},
		{
			name:      "list_400_fetch_invalid",
			fake:      &fakeVideoService{listErr: &googleapi.Error{Code: 400}},
			wantStage: stageFetchVideo,
			wantCat:   CatInvalid,
		},
		{
			name:      "update_400_invalid",
			fake:      &fakeVideoService{listResp: okList(), updateErr: &googleapi.Error{Code: 400}},
			wantStage: stageUpdateVideo,
			wantCat:   CatInvalid,
		},
		{
			name:      "update_403_quota",
			fake:      &fakeVideoService{listResp: okList(), updateErr: quota403},
			wantStage: stageUpdateVideo,
			wantCat:   CatQuota,
		},
		{
			name:      "update_403_not_owner_auth",
			fake:      &fakeVideoService{listResp: okList(), updateErr: other403},
			wantStage: stageUpdateVideo,
			wantCat:   CatAuth,
		},
		{
			name:      "update_404_invalid",
			fake:      &fakeVideoService{listResp: okList(), updateErr: &googleapi.Error{Code: 404}},
			wantStage: stageUpdateVideo,
			wantCat:   CatInvalid,
		},
		{
			name:      "update_500_network",
			fake:      &fakeVideoService{listResp: okList(), updateErr: &googleapi.Error{Code: 500}},
			wantStage: stageUpdateVideo,
			wantCat:   CatNetwork,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := &EditRequest{Channel: "test", VideoID: "v", Title: strPtr("T")}
			_, err := editMetadata(context.Background(), tc.fake, req)
			if err == nil {
				t.Fatal("want error, got nil")
			}
			var se *StageError
			if !errors.As(err, &se) {
				t.Fatalf("error is not *StageError: %v", err)
			}
			if se.Stage != tc.wantStage {
				t.Errorf("stage = %q, want %q", se.Stage, tc.wantStage)
			}
			if se.Category != tc.wantCat {
				t.Errorf("category = %q, want %q", se.Category, tc.wantCat)
			}
			if tc.wantHint != "" && !strings.Contains(se.Hint, tc.wantHint) {
				t.Errorf("hint = %q, want it to contain %q", se.Hint, tc.wantHint)
			}
		})
	}
}

// ---- Group 6: success projection through EditMetadata (via wire seam) -------

func TestEditMetadata_SuccessResult(t *testing.T) {
	fake := &fakeVideoService{
		listResp: &youtube.VideoListResponse{Items: []*youtube.Video{{Snippet: baseSnippet()}}},
	}
	res, err := editMetadata(context.Background(), fake, &EditRequest{
		Channel: "test", VideoID: "abc", Title: strPtr("Fixed"),
	})
	if err != nil {
		t.Fatalf("editMetadata: %v", err)
	}
	if res.VideoURL != "https://www.youtube.com/watch?v=abc" {
		t.Errorf("VideoURL = %q", res.VideoURL)
	}
	if !contains(res.UpdatedFields, "title") {
		t.Errorf("UpdatedFields %v missing title", res.UpdatedFields)
	}
}

// ---- Group 6b: EditMetadata request-level guards fast-fail (spend no quota) --
//
// The exported EditMetadata runs the request-level validate guards
// (channel-in-map, blank video_id, reject-clear-required) BEFORE it builds a
// YouTube client or makes any API call. This pins that property structurally:
// the Uploader is constructed with a valid channel alias whose refresh token is
// bogus, so if any of these requests advanced PAST validate to u.service /
// videos.list, the outcome would not be a clean StageError{validate, CatInvalid}.
// A validate/CatInvalid return therefore proves the guard fired before any
// quota-spending path — no List/Update, no network call.
func TestEditMetadata_ValidateFastFail_SpendsNoQuota(t *testing.T) {
	// A real Uploader with one configured channel ("main") but an unusable
	// token. store is nil on purpose: the edit path never touches the ledger,
	// and these reject cases return before any service is built, so a nil store
	// would be a loud failure (panic) if the guard order ever regressed.
	u := &Uploader{cfg: &config.Config{
		OAuth:    config.OAuthClient{ClientID: "cid", ClientSecret: "csecret"},
		Channels: map[string]*config.Channel{"main": {RefreshToken: "bogus-refresh-token"}},
	}}

	cases := []struct {
		name string
		req  *EditRequest
	}{
		{
			name: "unknown_channel_alias",
			req:  &EditRequest{Channel: "does-not-exist", VideoID: "vid123", Title: strPtr("New")},
		},
		{
			name: "blank_video_id",
			req:  &EditRequest{Channel: "main", VideoID: "   ", Title: strPtr("New")},
		},
		{
			name: "clear_required_title",
			req:  &EditRequest{Channel: "main", VideoID: "vid123", Title: strPtr("")},
		},
		{
			name: "clear_required_category",
			req:  &EditRequest{Channel: "main", VideoID: "vid123", CategoryID: strPtr("")},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := u.EditMetadata(context.Background(), tc.req)
			if err == nil {
				t.Fatalf("want a validate error, got result %+v", res)
			}
			var se *StageError
			if !errors.As(err, &se) {
				t.Fatalf("error is not *StageError: %v", err)
			}
			if se.Stage != stageValidate {
				t.Errorf("stage = %q, want %q (guard must fire before any API call)", se.Stage, stageValidate)
			}
			if se.Category != CatInvalid {
				t.Errorf("category = %q, want %q", se.Category, CatInvalid)
			}
		})
	}
}

// ---- Group 7: validator parity — normalize and the edit path agree ----------
//
// The shared validators (validateTitle, validateCategoryNumeric,
// validateTagsBudget) back BOTH normalize and mergeSnippet. This pins that they
// accept/reject identically, so the reviewed upload path and the edit path can
// never drift apart.

func TestValidatorParity(t *testing.T) {
	titles := []struct {
		in   string
		want bool // true => valid
	}{
		{"Good Title", true},
		{"", false},
		{strings.Repeat("x", 101), false},
		{"bad <br>", false},
	}
	for _, c := range titles {
		if (validateTitle(c.in) == nil) != c.want {
			t.Errorf("validateTitle(%q) valid=%v, want %v", c.in, validateTitle(c.in) == nil, c.want)
		}
	}

	cats := []struct {
		in   string
		want bool
	}{{"22", true}, {"28", true}, {"Music", false}, {"", false}}
	for _, c := range cats {
		if (validateCategoryNumeric(c.in) == nil) != c.want {
			t.Errorf("validateCategoryNumeric(%q) valid=%v, want %v", c.in, validateCategoryNumeric(c.in) == nil, c.want)
		}
	}

	// Tag budget parity: the exact 500/501 boundary the upload path uses.
	if validateTagsBudget([]string{strings.Repeat("x", 500)}) != nil {
		t.Error("validateTagsBudget rejected a 500-char set; want accept")
	}
	if validateTagsBudget([]string{strings.Repeat("x", 501)}) == nil {
		t.Error("validateTagsBudget accepted a 501-char set; want reject")
	}
}
