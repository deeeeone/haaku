package users

import (
	"testing"
	"time"

	"trakt-api/internal/database"
)

func TestDefaultSettings(t *testing.T) {
	s := defaultSettings()
	if s.Privacy.Profile != "public" || s.Privacy.History != "friends" {
		t.Errorf("privacy defaults wrong: %+v", s.Privacy)
	}
	if s.Privacy.Ratings != "public" || s.Privacy.Activity != "friends" {
		t.Errorf("privacy defaults wrong: %+v", s.Privacy)
	}
	if !s.Notifications.Follows || !s.Notifications.Replies || !s.Notifications.ImportComplete {
		t.Errorf("notifications defaults wrong: %+v", s.Notifications)
	}
	if s.Notifications.WeeklyDigest {
		t.Error("weekly_digest should default to false")
	}
}

func TestParseSettingsDefaultsOnEmpty(t *testing.T) {
	for _, raw := range []string{"", "{}", "null", "not-json"} {
		got := parseSettings(raw)
		if got.Privacy.Profile != "public" {
			t.Errorf("parseSettings(%q) Profile = %q, want default 'public'", raw, got.Privacy.Profile)
		}
		if !got.Notifications.Follows {
			t.Errorf("parseSettings(%q) Follows = false, want default true", raw)
		}
	}
}

func TestParseSettingsOverridesAndBackfills(t *testing.T) {
	raw := `{"privacy":{"profile":"private"}}`
	got := parseSettings(raw)
	if got.Privacy.Profile != "private" {
		t.Errorf("Profile = %q, want private", got.Privacy.Profile)
	}
	// Missing privacy fields are backfilled from defaults.
	if got.Privacy.History != "friends" || got.Privacy.Ratings != "public" || got.Privacy.Activity != "friends" {
		t.Errorf("backfill failed: %+v", got.Privacy)
	}
	// Note (current impl behavior): parseSettings backfills privacy fields only,
	// not the notifications block — a partial JSON leaves Notifications zeroed.
	if got.Notifications.Follows {
		t.Error("Notifications block is not backfilled by parseSettings (documented current behavior)")
	}
}

func TestParseSettingsFullOverride(t *testing.T) {
	raw := `{"privacy":{"profile":"friends","history":"public","ratings":"private","activity":"public"},"notifications":{"follows":false,"replies":false,"import_complete":false,"weekly_digest":true}}`
	got := parseSettings(raw)
	if got.Privacy.Profile != "friends" || got.Privacy.Activity != "public" {
		t.Errorf("privacy override wrong: %+v", got.Privacy)
	}
	if got.Notifications.Follows || got.Notifications.WeeklyDigest == false {
		t.Errorf("notifications override wrong: %+v", got.Notifications)
	}
}

func TestBuildProfileResponse(t *testing.T) {
	created := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	u := &database.User{
		ID:          "user-9",
		Username:    "alice",
		Name:        "Alice",
		Bio:         "hi",
		AvatarURL:   "https://example.com/a.png",
		Location:    "NYC",
		Timezone:    "America/New_York",
		Language:    "en",
		IsPrivate:   true,
		IsAdmin:     true,
		IsModerator: false,
		Status:      "active",
		Plan:        "free",
		CreatedAt:   created,
	}
	got := buildProfileResponse(u)
	if got.ID != "user-9" || got.Username != "alice" || got.Name != "Alice" {
		t.Errorf("profile base wrong: %+v", got)
	}
	if got.Bio != "hi" || got.AvatarURL != "https://example.com/a.png" || got.Location != "NYC" {
		t.Errorf("profile content wrong: %+v", got)
	}
	if got.Timezone != "America/New_York" || got.Language != "en" {
		t.Errorf("profile locale wrong: %+v", got)
	}
	if !got.IsPrivate || !got.IsAdmin || got.IsModerator {
		t.Errorf("profile flags wrong: %+v", got)
	}
	if got.Status != "active" || got.Plan != "free" {
		t.Errorf("profile account fields wrong: %+v", got)
	}
	if got.CreatedAt != "2026-02-01T12:00:00.000Z" {
		t.Errorf("CreatedAt = %q", got.CreatedAt)
	}
}

func TestBuildPublicProfileResponse(t *testing.T) {
	u := &database.User{ID: "u", Username: "bob", Name: "Bob", Bio: "b", IsPrivate: true}
	got := buildPublicProfileResponse(u)
	if got.ID != "u" || got.Username != "bob" || got.Name != "Bob" || got.Bio != "b" {
		t.Errorf("public profile wrong: %+v", got)
	}
	if !got.IsPrivate {
		t.Error("IsPrivate should carry through")
	}
}
