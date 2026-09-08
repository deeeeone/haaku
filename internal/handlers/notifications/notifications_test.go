package notifications

import (
	"testing"
	"time"

	"trakt-api/internal/database"
)

func TestParseNotifSettingsDefaultsOnEmpty(t *testing.T) {
	for _, raw := range []string{"", "{}", "not-json"} {
		got := parseNotifSettings(raw)
		if !got.Follows || !got.Replies || !got.ListInvite || !got.ImportComplete {
			t.Errorf("parseNotifSettings(%q) = %+v, want all default true", raw, got)
		}
	}
}

func TestParseNotifSettingsPartialOverride(t *testing.T) {
	got := parseNotifSettings(`{"notifications":{"follows":false}}`)
	if got.Follows {
		t.Error("Follows should be false (explicitly set)")
	}
	// Unset fields retain defaults.
	if !got.Replies || !got.ListInvite || !got.ImportComplete {
		t.Errorf("unset fields should default true, got %+v", got)
	}
}

func TestParseNotifSettingsFullOverride(t *testing.T) {
	got := parseNotifSettings(`{"notifications":{"follows":false,"replies":false,"list_invite":false,"import_complete":false}}`)
	if got.Follows || got.Replies || got.ListInvite || got.ImportComplete {
		t.Errorf("all toggles should be false, got %+v", got)
	}
}

func TestPrefKeyForType(t *testing.T) {
	cases := map[string]string{
		NotifTypeFollow:     "follows",
		NotifTypeReply:      "replies",
		NotifTypeListInvite: "list_invite",
		NotifTypeImportDone: "import_complete",
		"mention":           "", // no toggle — always allowed
		"unknown":           "",
		"":                  "",
	}
	for notifType, want := range cases {
		if got := prefKeyForType(notifType); got != want {
			t.Errorf("prefKeyForType(%q) = %q, want %q", notifType, got, want)
		}
	}
}

func TestToNotificationItem(t *testing.T) {
	created := time.Date(2026, 3, 1, 8, 30, 0, 0, time.UTC)
	n := &database.Notification{
		ID:        5,
		UserID:    "u1",
		Type:      NotifTypeFollow,
		ActorID:   "u2",
		Payload:   database.JSONMap{"who": "u2"},
		Read:      true,
		CreatedAt: created,
	}
	got := toNotificationItem(n)
	if got.ID != 5 || got.UserID != "u1" || got.Type != NotifTypeFollow || got.ActorID != "u2" {
		t.Errorf("notification base wrong: %+v", got)
	}
	if !got.Read {
		t.Error("Read should carry through")
	}
	if got.Payload["who"] != "u2" {
		t.Errorf("Payload = %v", got.Payload)
	}
	if got.CreatedAt != "2026-03-01T08:30:00.000Z" {
		t.Errorf("CreatedAt = %q", got.CreatedAt)
	}
}
