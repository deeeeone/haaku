package updates

import (
	"net/http"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupPeopleIDsDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.Person{}); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

func TestPeopleUpdatedIDsPagination(t *testing.T) {
	db := setupPeopleIDsDB(t)
	now := time.Now().UTC()

	first := database.Person{TmdbID: 101, Name: "First"}
	second := database.Person{TmdbID: 102, Name: "Second"}
	old := database.Person{TmdbID: 103, Name: "Old"}
	for _, person := range []*database.Person{&first, &second, &old} {
		if err := db.Create(person).Error; err != nil {
			t.Fatalf("seed person: %v", err)
		}
	}
	db.Model(&database.Person{}).Where("id = ?", first.ID).UpdateColumn("updated_at", now.Add(-2*time.Hour))
	db.Model(&database.Person{}).Where("id = ?", second.ID).UpdateColumn("updated_at", now.Add(-1*time.Hour))
	db.Model(&database.Person{}).Where("id = ?", old.ID).UpdateColumn("updated_at", now.AddDate(0, 0, -10))

	since := now.AddDate(0, 0, -1).Format("2006-01-02")
	app := idsApp()

	page1 := doIDsGet(t, app, "/v1/people/updates/id/"+since+"?page=1&limit=1")
	if page1.StatusCode != http.StatusOK {
		t.Fatalf("page 1 status = %d, want 200", page1.StatusCode)
	}
	if got := page1.Header.Get("X-Pagination-Total-Items"); got != "2" {
		t.Fatalf("total items = %q, want 2", got)
	}
	if got := page1.Header.Get("X-Pagination-Page"); got != "1" {
		t.Fatalf("page header = %q, want 1", got)
	}
	if got := page1.Header.Get("X-Pagination-Limit"); got != "1" {
		t.Fatalf("limit header = %q, want 1", got)
	}
	if got := page1.Header.Get("X-Pagination-Total-Pages"); got != "2" {
		t.Fatalf("total pages = %q, want 2", got)
	}
	if ids := decodeIDs(t, page1); len(ids) != 1 || ids[0] != first.ID {
		t.Fatalf("page 1 ids = %v, want [%d]", ids, first.ID)
	}

	page2 := doIDsGet(t, app, "/v1/people/updates/id/"+since+"?page=2&limit=1")
	if page2.StatusCode != http.StatusOK {
		t.Fatalf("page 2 status = %d, want 200", page2.StatusCode)
	}
	if ids := decodeIDs(t, page2); len(ids) != 1 || ids[0] != second.ID {
		t.Fatalf("page 2 ids = %v, want [%d]", ids, second.ID)
	}

	future := now.AddDate(1, 0, 0).Format("2006-01-02")
	empty := doIDsGet(t, app, "/v1/people/updates/id/"+future)
	if ids := decodeIDs(t, empty); len(ids) != 0 {
		t.Fatalf("future window ids = %v, want []", ids)
	}

	bad := doIDsGet(t, app, "/v1/people/updates/id/not-a-date")
	defer func() { _ = bad.Body.Close() }()
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid date status = %d, want 400", bad.StatusCode)
	}
}
