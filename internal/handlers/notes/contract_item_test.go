package notes

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newNoteTestApp(t *testing.T, userID string) *fiber.App {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.Movie{}, &database.Season{}, &database.Episode{}, &database.UserNote{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	database.SetDBForTest(t, db)
	t.Cleanup(func() {})

	app := fiber.New(fiber.Config{ErrorHandler: response.HandleError})
	app.Get("/v1/notes/:id/item", func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return handleGetNoteItem(c)
	})
	return app
}

func TestNoteItemReturnsTheAttachedMovieRow(t *testing.T) {
	app := newNoteTestApp(t, "owner")
	db, _ := database.GetDB()

	movie := database.Movie{ID: 550, Title: "Fight Club"}
	if err := db.Create(&movie).Error; err != nil {
		t.Fatalf("create movie: %v", err)
	}
	note := database.UserNote{UserID: "owner", MediaType: "movie", MediaID: movie.ID, Note: "watch"}
	if err := db.Create(&note).Error; err != nil {
		t.Fatalf("create note: %v", err)
	}

	status, body := getJSON(t, app, "/v1/notes/"+strconv.FormatInt(note.ID, 10)+"/item")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}
	var got struct {
		Type  string `json:"type"`
		Movie struct {
			Title string `json:"title"`
			IDs   struct {
				Trakt int64 `json:"trakt"`
			} `json:"ids"`
		} `json:"movie"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Type != "movie" || got.Movie.Title != "Fight Club" || got.Movie.IDs.Trakt != 550 {
		t.Fatalf("item = %+v, want the movie row", got)
	}
}

func TestNoteItemIsOwnerOnlyAnd404sWithoutARow(t *testing.T) {
	app := newNoteTestApp(t, "someone-else")
	db, _ := database.GetDB()

	note := database.UserNote{UserID: "owner", MediaType: "movie", MediaID: 1, Note: "private"}
	if err := db.Create(&note).Error; err != nil {
		t.Fatalf("create note: %v", err)
	}
	if status, body := getJSON(t, app, "/v1/notes/"+strconv.FormatInt(note.ID, 10)+"/item"); status != http.StatusForbidden {
		t.Fatalf("other user's note: status = %d, want 403 (%s)", status, body)
	}

	own := database.UserNote{UserID: "someone-else", MediaType: "movie", MediaID: 999999, Note: "orphan"}
	if err := db.Create(&own).Error; err != nil {
		t.Fatalf("create orphan note: %v", err)
	}
	if status, body := getJSON(t, app, "/v1/notes/"+strconv.FormatInt(own.ID, 10)+"/item"); status != http.StatusNotFound {
		t.Fatalf("missing media row: status = %d, want 404 (%s)", status, body)
	}
}

func getJSON(t *testing.T, app *fiber.App, path string) (int, string) {
	t.Helper()
	req, err := http.NewRequest("GET", path, strings.NewReader(""))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Host = "example.com"
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var raw json.RawMessage
	_ = json.NewDecoder(resp.Body).Decode(&raw)
	return resp.StatusCode, string(raw)
}
