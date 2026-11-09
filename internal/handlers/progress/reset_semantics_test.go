package progress

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"
)

// Trakt's POST reset records a point; it never deletes history. Watched
// progress then ignores plays before that point, and DELETE removes the point so
// the previous progress comes back.
func TestResetKeepsHistoryAndUndoRestoresProgress(t *testing.T) {
	db := setupShowProgressDB(t)
	showID := seedShow(t, db, 7101, "Rewatch")
	episodeIDs := seedSeasonWithEpisodes(t, db, showID, 1, []epSeed{{1, airedDaysAgo(5)}, {2, airedDaysAgo(4)}})

	watchedAt := time.Now().UTC().AddDate(0, 0, -3)
	for _, id := range episodeIDs {
		if err := db.Create(&database.UserHistory{
			UserID: "user-1", MediaType: "episode", MediaID: id, WatchedAt: watchedAt, Source: "api",
		}).Error; err != nil {
			t.Fatalf("seed history: %v", err)
		}
	}

	app := showProgressApp("user-1")
	resp := doProgressReq(t, app, http.MethodPost, "/shows/"+strconv.FormatInt(showID, 10)+"/progress/watched/reset")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("reset status = %d, want 201", resp.StatusCode)
	}
	var ack struct {
		ResetAt string `json:"reset_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ack); err != nil {
		t.Fatalf("decode reset ack: %v", err)
	}
	_ = resp.Body.Close()
	if ack.ResetAt == "" {
		t.Error("reset response should carry reset_at")
	}

	count := func() int64 {
		var n int64
		if err := db.Model(&database.UserHistory{}).
			Where("user_id = ? AND media_type = ? AND media_id IN ?", "user-1", "episode", episodeIDs).
			Count(&n).Error; err != nil {
			t.Fatalf("count history: %v", err)
		}
		return n
	}
	if n := count(); n != 2 {
		t.Fatalf("history rows after reset = %d, want 2 (reset must not delete history)", n)
	}

	got := decodeShowProgress(t, doProgressReq(t, app, http.MethodGet, "/shows/"+strconv.FormatInt(showID, 10)+"/progress/watched"))
	if got.Aired != 2 || got.Completed != 0 {
		t.Fatalf("after reset aired/completed = %d/%d, want 2/0", got.Aired, got.Completed)
	}
	if got.LastWatchedAt != nil {
		t.Errorf("last_watched_at = %v, want null while all plays are before reset_at", *got.LastWatchedAt)
	}

	resp = doProgressReq(t, app, http.MethodDelete, "/shows/"+strconv.FormatInt(showID, 10)+"/progress/watched/reset")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("undo status = %d, want 204", resp.StatusCode)
	}
	_ = resp.Body.Close()

	got = decodeShowProgress(t, doProgressReq(t, app, http.MethodGet, "/shows/"+strconv.FormatInt(showID, 10)+"/progress/watched"))
	if got.Completed != 2 {
		t.Fatalf("after undo completed = %d, want 2", got.Completed)
	}
	if n := count(); n != 2 {
		t.Errorf("history rows after undo = %d, want 2", n)
	}

	// Undoing when no reset exists is still 204 (idempotent), per the contract.
	resp = doProgressReq(t, app, http.MethodDelete, "/shows/"+strconv.FormatInt(showID, 10)+"/progress/watched/reset")
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("second undo status = %d, want 204", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// The optional body moves the reset point: an earlier reset_at keeps plays
// after it.
func TestResetWithEarlierResetAtKeepsLaterPlays(t *testing.T) {
	db := setupShowProgressDB(t)
	showID := seedShow(t, db, 7102, "Partial Rewatch")
	episodeIDs := seedSeasonWithEpisodes(t, db, showID, 1, []epSeed{{1, airedDaysAgo(6)}, {2, airedDaysAgo(1)}})

	if err := db.Create(&database.UserHistory{
		UserID: "user-1", MediaType: "episode", MediaID: episodeIDs[0], WatchedAt: time.Now().UTC().AddDate(0, 0, -5), Source: "api",
	}).Error; err != nil {
		t.Fatalf("seed old history: %v", err)
	}
	if err := db.Create(&database.UserHistory{
		UserID: "user-1", MediaType: "episode", MediaID: episodeIDs[1], WatchedAt: time.Now().UTC().AddDate(0, 0, -1), Source: "api",
	}).Error; err != nil {
		t.Fatalf("seed new history: %v", err)
	}

	resetAt := time.Now().UTC().AddDate(0, 0, -2).Format(time.RFC3339)
	body, _ := json.Marshal(map[string]string{"reset_at": resetAt})
	req, _ := http.NewRequest(http.MethodPost, "/shows/"+strconv.FormatInt(showID, 10)+"/progress/watched/reset", bytes.NewReader(body))
	req.Host = "example.com"
	req.Header.Set("Content-Type", "application/json")
	resp, err := showProgressApp("user-1").Test(req)
	if err != nil {
		t.Fatalf("reset request: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("reset status = %d, want 201", resp.StatusCode)
	}
	_ = resp.Body.Close()

	got := decodeShowProgress(t, doProgressReq(t, showProgressApp("user-1"), http.MethodGet,
		"/shows/"+strconv.FormatInt(showID, 10)+"/progress/watched"))
	if got.Completed != 1 {
		t.Errorf("completed = %d, want 1 (only the play after reset_at)", got.Completed)
	}
}
