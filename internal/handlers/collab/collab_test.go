package collab

import (
	"testing"
)

func TestParseID(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"42", 42},
		{"0", 0},
		{"-5", -5},  // parses negative (caller validates positivity)
		{"abc", 0},  // invalid → 0
		{"", 0},     // empty → 0
		{"12.5", 0}, // non-integer → 0
		{"  42", 0}, // whitespace not allowed → 0
	}
	for _, tc := range cases {
		if got := parseID(tc.in); got != tc.want {
			t.Errorf("parseID(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
