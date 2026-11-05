package mode

import "testing"

// TestParseDefaultsToAPI asserts an unset variable selects the serving mode,
// which is what every existing deployment relies on.
func TestParseDefaultsToAPI(t *testing.T) {
	for _, raw := range []string{"", "  ", "api", "API", " Api "} {
		got, err := Parse(raw)
		if err != nil {
			t.Errorf("Parse(%q) error = %v", raw, err)
			continue
		}
		if got != API {
			t.Errorf("Parse(%q) = %q, want %q", raw, got, API)
		}
	}
}

// TestParseKnownModes covers the accepted values.
func TestParseKnownModes(t *testing.T) {
	cases := map[string]Mode{
		"worker":  Worker,
		"WORKER":  Worker,
		"migrate": Migrate,
	}

	for raw, want := range cases {
		got, err := Parse(raw)
		if err != nil {
			t.Errorf("Parse(%q) error = %v", raw, err)
			continue
		}
		if got != want {
			t.Errorf("Parse(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TestParseRejectsUnknown asserts a typo fails loudly instead of silently
// starting the API. A misspelled TKILLER_MODE that quietly served traffic would
// be a very expensive kind of wrong.
func TestParseRejectsUnknown(t *testing.T) {
	for _, raw := range []string{"worker2", "false", "0", "all", "proxy"} {
		if _, err := Parse(raw); err == nil {
			t.Errorf("Parse(%q) returned no error, want one", raw)
		}
	}
}

// TestMayMigrate asserts workers cannot change the schema, which is the property
// that makes running several workers against one database safe.
func TestMayMigrate(t *testing.T) {
	if Worker.MayMigrate() {
		t.Error("Worker.MayMigrate() = true, want false")
	}
	if !API.MayMigrate() {
		t.Error("API.MayMigrate() = false, want true")
	}
	if !Migrate.MayMigrate() {
		t.Error("Migrate.MayMigrate() = false, want true")
	}
}

// TestRolePredicates pins the mode table so a new mode cannot be added without
// deciding what it does.
func TestRolePredicates(t *testing.T) {
	cases := []struct {
		mode        Mode
		servesHTTP  bool
		runsWorkers bool
	}{
		{API, true, true},
		{Worker, false, true},
		{Migrate, false, false},
	}

	for _, tc := range cases {
		if got := tc.mode.ServesHTTP(); got != tc.servesHTTP {
			t.Errorf("%s.ServesHTTP() = %v, want %v", tc.mode, got, tc.servesHTTP)
		}
		if got := tc.mode.RunsWorkers(); got != tc.runsWorkers {
			t.Errorf("%s.RunsWorkers() = %v, want %v", tc.mode, got, tc.runsWorkers)
		}
	}
}
