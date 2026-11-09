package database

import (
	"reflect"
	"testing"
)

// A TEXT[] column receives a PostgreSQL array literal, not a JSON array; GORM
// handing it `["profile"]` is what made every OAuth token INSERT fail with
// SQLSTATE 22P02. This pins the write encoding and the read fallback.
func TestTextArrayValueIsPostgresLiteral(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   TextArray
		want string
	}{
		{"single", TextArray{"profile"}, `{"profile"}`},
		{"many", TextArray{"profile", "history:read"}, `{"profile","history:read"}`},
		{"empty", TextArray{}, `{}`},
		{"quote", TextArray{`a"b`}, `{"a\"b"}`},
		{"backslash", TextArray{`a\b`}, `{"a\\b"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := tc.in.Value()
			if err != nil {
				t.Fatalf("Value: %v", err)
			}
			if v != tc.want {
				t.Errorf("Value = %v, want %v", v, tc.want)
			}
		})
	}
}

func TestTextArrayScanAcceptsBothEncodings(t *testing.T) {
	var pgArray TextArray
	if err := pgArray.Scan("{profile,history:read}"); err != nil {
		t.Fatalf("Scan pg literal: %v", err)
	}
	if want := (TextArray{"profile", "history:read"}); !reflect.DeepEqual(pgArray, want) {
		t.Errorf("pg literal = %v, want %v", pgArray, want)
	}

	var jsonArray TextArray
	if err := jsonArray.Scan([]byte(`["profile"]`)); err != nil {
		t.Fatalf("Scan json: %v", err)
	}
	if want := (TextArray{"profile"}); !reflect.DeepEqual(jsonArray, want) {
		t.Errorf("json = %v, want %v", jsonArray, want)
	}
}

// The round trip is what the token INSERT + later refresh read relies on.
func TestTextArrayRoundTrip(t *testing.T) {
	in := TextArray{"profile", "watchlist:write"}
	v, err := in.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	var out TextArray
	if err := out.Scan(v); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round trip = %v, want %v", out, in)
	}
}
