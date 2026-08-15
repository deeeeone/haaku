package database

import "testing"

func TestJSONMapStringValue(t *testing.T) {
	tests := []struct {
		name string
		m    JSONMap
		key  string
		want string
	}{
		{"nil map", nil, "poster", ""},
		{"missing key", JSONMap{}, "poster", ""},
		{"nil value", JSONMap{"poster": nil}, "poster", ""},
		{"non-string value", JSONMap{"poster": 42}, "poster", ""},
		{"string value", JSONMap{"poster": "/path.jpg"}, "poster", "/path.jpg"},
		{"empty string", JSONMap{"poster": ""}, "poster", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.StringValue(tt.key); got != tt.want {
				t.Errorf("StringValue(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestStringSliceValues(t *testing.T) {
	tests := []struct {
		name string
		s    StringSlice
		want []string
	}{
		{"nil", nil, []string{}},
		{"empty", StringSlice{}, []string{}},
		{"values", StringSlice{"a", "b"}, []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.s.Values()
			if got == nil {
				t.Fatal("Values() returned nil")
			}
			if len(got) != len(tt.want) {
				t.Fatalf("Values() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("Values()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
