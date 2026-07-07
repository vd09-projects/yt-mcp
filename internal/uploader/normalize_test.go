package uploader

import (
	"strings"
	"testing"
	"unicode/utf8"

	"yt-mcp/internal/config"
)

func boolPtr(b bool) *bool { return &b }

// TestTagsBudget covers the pure tag-budget arithmetic in two labelled groups:
//
//	Group 1 (characterization) pins the comma (n-1) and +2 spaced-tag quoting
//	and the > 500 threshold on ASCII input, so a later refactor cannot silently
//	change the math. internal/uploader shipped with zero tests, so this table
//	*is* the regression guard, not a supplement to existing coverage.
//
//	Group 2 (rune axis — Decision D1) proves the budget counts characters
//	(runes), not bytes: multi-byte input whose byte length exceeds 500 but whose
//	rune length does not is accepted; rune length over 500 is rejected. These
//	cases fail under the old len() guard and are the point of D1. The old byte
//	behaviour is deliberately NOT pinned.
func TestTagsBudget(t *testing.T) {
	cases := []struct {
		group string
		name  string
		tags  []string
		want  int // computed budget; over-limit is want > 500
	}{
		// --- Group 1: characterization (ASCII arithmetic) ---
		{
			// (a) five hyphenated (no-space) tags: 4*99 + 100 = 496 chars,
			// + (5-1) commas = 500 exactly -> at the boundary, accepted.
			group: "characterization",
			name:  "a_hyphenated_exactly_500_accept",
			tags: []string{
				strings.Repeat("a", 99),
				strings.Repeat("b", 99),
				strings.Repeat("c", 99),
				strings.Repeat("d", 99),
				strings.Repeat("e", 100),
			},
			want: 500,
		},
		{
			// (b) same content, last tag now contains a space: +2 for the
			// quotes YouTube wraps it in -> 502, over budget, rejected.
			group: "characterization",
			name:  "b_spaced_tag_pushes_over_reject",
			tags: []string{
				strings.Repeat("a", 99),
				strings.Repeat("b", 99),
				strings.Repeat("c", 99),
				strings.Repeat("d", 99),
				strings.Repeat("e", 98) + " f", // 100 chars incl. the space, +2 for quotes
			},
			want: 502,
		},
		{
			// (c) comma-accounting boundary: 500 chars in ONE tag has no comma
			// (500, accept), but splitting the same 500 chars across two tags
			// adds one comma -> 501, reject. The (n-1) term is the decider.
			group: "characterization",
			name:  "c_single_500_no_comma_accept",
			tags:  []string{strings.Repeat("x", 500)},
			want:  500,
		},
		{
			group: "characterization",
			name:  "c_two_tags_comma_pushes_over_reject",
			tags:  []string{strings.Repeat("x", 250), strings.Repeat("y", 250)},
			want:  501,
		},
		{
			// (d) single short tag: no comma term.
			group: "characterization",
			name:  "d_single_short_tag_accept",
			tags:  []string{"hello"},
			want:  5,
		},
		{
			group: "characterization",
			name:  "d_empty_slice_accept",
			tags:  []string{},
			want:  0,
		},
		{
			group: "characterization",
			name:  "d_nil_slice_accept",
			tags:  nil,
			want:  0,
		},

		// --- Group 2: rune axis (Decision D1) ---
		{
			// (e) 200 CJK runes = 600 bytes but 200 characters; single tag ->
			// budget 200 <= 500, accepted. Fails under the old byte guard.
			group: "rune-axis-D1",
			name:  "e_bytes_over_500_runes_under_accept",
			tags:  []string{strings.Repeat("あ", 200)},
			want:  200,
		},
		{
			// (f) 501 CJK runes -> budget 501 > 500, rejected on characters.
			group: "rune-axis-D1",
			name:  "f_runes_over_500_reject",
			tags:  []string{strings.Repeat("あ", 501)},
			want:  501,
		},
	}

	for _, tc := range cases {
		t.Run(tc.group+"/"+tc.name, func(t *testing.T) {
			got := tagsBudget(tc.tags)
			if got != tc.want {
				t.Fatalf("tagsBudget(%s) = %d, want %d", tc.name, got, tc.want)
			}
			// Cross-check the accept/reject decision the guard actually makes.
			overLimit := got > 500
			wantOver := tc.want > 500
			if overLimit != wantOver {
				t.Fatalf("over-limit decision = %v, want %v (budget %d)", overLimit, wantOver, got)
			}
		})
	}
}

// TestTagsBudget_RuneVsByteDivergence documents the D1 invariant explicitly: the
// accept case's byte length really does exceed 500 (so the old len() guard would
// have wrongly rejected it) while its rune length does not.
func TestTagsBudget_RuneVsByteDivergence(t *testing.T) {
	tag := strings.Repeat("あ", 200)
	if bytes := len(tag); bytes <= 500 {
		t.Fatalf("test setup: want byte length > 500 to exercise D1, got %d", bytes)
	}
	if runes := utf8.RuneCountInString(tag); runes > 500 {
		t.Fatalf("test setup: want rune length <= 500, got %d", runes)
	}
	if b := tagsBudget([]string{tag}); b > 500 {
		t.Fatalf("D1 regression: multi-byte tag under 500 runes rejected (budget %d)", b)
	}
}

// TestNormalizeTagBudget wires the guard through normalize itself, so the pure
// helper cannot be correct while its integration into the validate stage drifts.
// One accept case and one reject case are enough to pin the wiring + the error
// shape (stage=validate, category=invalid_request, "characters" wording, Hint).
func TestNormalizeTagBudget(t *testing.T) {
	baseReq := func(tags []string) *Request {
		return &Request{
			Channel:       "test",
			Source:        "https://example.com/v.mp4",
			Title:         "A valid title",
			Description:   "A valid description.",
			Tags:          tags,
			CategoryID:    "22",
			PrivacyStatus: "unlisted",
			MadeForKids:   boolPtr(false),
		}
	}
	ch := &config.Channel{}

	t.Run("accept_exactly_500", func(t *testing.T) {
		// 500 chars in one tag -> budget 500, at boundary, accepted.
		req := baseReq([]string{strings.Repeat("x", 500)})
		if _, se := normalize(req, ch); se != nil {
			t.Fatalf("normalize rejected a 500-char tag set: %v", se)
		}
	})

	t.Run("reject_over_500_multibyte", func(t *testing.T) {
		// 501 CJK runes: byte length far over 500, rune length just over.
		req := baseReq([]string{strings.Repeat("あ", 501)})
		_, se := normalize(req, ch)
		if se == nil {
			t.Fatal("normalize accepted a 501-character tag set; want rejection")
		}
		if se.Stage != "validate" {
			t.Errorf("stage = %q, want %q", se.Stage, "validate")
		}
		if se.Category != CatInvalid {
			t.Errorf("category = %q, want %q", se.Category, CatInvalid)
		}
		if !strings.Contains(se.Err.Error(), "characters") {
			t.Errorf("message = %q, want it to mention \"characters\"", se.Err.Error())
		}
		if se.Hint == "" {
			t.Error("Hint is empty; want budget-accounting guidance that round-trips to the caller")
		}
	})
}
