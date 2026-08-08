package lists

import "testing"

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
