package lists

import "testing"

func TestGenerateSlugNoPanicOnShortUserID(t *testing.T) {
	cases := []struct {
		name   string
		userID string
	}{
		{"My List", ""},
		{"My List", "ab"},
		{"My List", "abc12345"},
		{"My List", "abcdefghijklmnop"},
	}
	for _, tc := range cases {
		got := generateSlug(tc.name, tc.userID)
		if got == "" {
			t.Errorf("generateSlug(%q, %q) = empty", tc.name, tc.userID)
		}
	}
}

func TestGenerateSlugUsesFirst8OfUserID(t *testing.T) {
	if got := generateSlug("My List", "abcdefghijkl"); got != "my-list-abcdefgh" {
		t.Errorf("generateSlug = %q, want %q", got, "my-list-abcdefgh")
	}
	if got := generateSlug("My List", "abcd"); got != "my-list-abcd" {
		t.Errorf("generateSlug short id = %q, want %q", got, "my-list-abcd")
	}
	if got := generateSlug("My List", ""); got != "my-list" {
		t.Errorf("generateSlug empty id = %q, want %q", got, "my-list")
	}
}

func TestResolveListItemSort(t *testing.T) {
	cases := []struct {
		sortBy  string
		sortHow string
		wantCol string
		wantDir string
	}{
		{"rank", "asc", "rank", "ASC"},
		{"rank", "", "rank", "ASC"},
		{"added", "desc", "added_at", "DESC"},
		{"added", "ASC", "added_at", "ASC"},
		{"media_type", "desc", "media_type", "DESC"},
		{"media_id", "", "media_id", "ASC"},
		{"notes", "desc", "notes", "DESC"},
		// Unknown keys — including SQL injection attempts — fall back to rank.
		{"title", "desc", "rank", "DESC"},
		{"rank; DROP TABLE list_items;--", "asc", "rank", "ASC"},
		{"(SELECT pg_sleep(5))", "desc", "rank", "DESC"},
		// sort_how is normalized to ASC/DESC.
		{"rank", "random", "rank", "ASC"},
		{"rank", "DESC", "rank", "DESC"},
	}
	for _, tc := range cases {
		col, dir := resolveListItemSort(tc.sortBy, tc.sortHow)
		if col != tc.wantCol || dir != tc.wantDir {
			t.Errorf("resolveListItemSort(%q, %q) = (%q, %q), want (%q, %q)",
				tc.sortBy, tc.sortHow, col, dir, tc.wantCol, tc.wantDir)
		}
	}
}
