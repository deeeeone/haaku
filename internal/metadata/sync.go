package metadata

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/haaku-tv/haaku/external/tmdb"

	"gorm.io/gorm"
)

// SinceFile is the file that stores the last sync date.
const SinceFileName = "metadata_sync_since.txt"

// LoadSince reads the last sync date from a file.
// Returns empty string if the file doesn't exist.
func LoadSince(sinceFile string) (string, error) {
	data, err := os.ReadFile(sinceFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read since file %s: %w", sinceFile, err)
	}
	s := string(data)
	if len(s) > 0 {
		// Trim trailing newline
		if s[len(s)-1] == '\n' {
			s = s[:len(s)-1]
		}
	}
	return s, nil
}

// SaveSince writes the sync date to a file.
func SaveSince(sinceFile, date string) error {
	dir := filepath.Dir(sinceFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create since dir %s: %w", dir, err)
	}
	return os.WriteFile(sinceFile, []byte(date+"\n"), 0644)
}

// RunSync polls all TMDB change feeds and processes changed items.
// Updates the since file after a successful run.
func RunSync(client *tmdb.Client, db *gorm.DB, sinceFile string) error {
	since, err := LoadSince(sinceFile)
	if err != nil {
		return fmt.Errorf("load since: %w", err)
	}

	if since == "" {
		// Default to 24 hours ago on first run.
		since = time.Now().Add(-24 * time.Hour).Format("2006-01-02")
		slog.Info("[sync] no since date, defaulting", "since", since)
	}

	slog.Info("[sync] starting change feed poll", "since", since)

	today := time.Now().Format("2006-01-02")

	if err := syncMovies(client, db, since); err != nil {
		slog.Error("[sync] movie sync failed", "error", err)
	}
	if err := syncShows(client, db, since); err != nil {
		slog.Error("[sync] show sync failed", "error", err)
	}
	if err := syncPeople(client, db, since); err != nil {
		slog.Error("[sync] people sync failed", "error", err)
	}

	// Save the new since date for the next run.
	if err := SaveSince(sinceFile, today); err != nil {
		slog.Error("[sync] failed to save since date", "error", err)
		return err
	}

	slog.Info("[sync] complete", "new_since", today)
	return nil
}

// syncMovies processes the movie change feed.
func syncMovies(client *tmdb.Client, db *gorm.DB, since string) error {
	return syncChangeFeed(client, db, since, "movie", func(c *tmdb.Client, id int) (*tmdb.Movie, error) {
		return c.GetMovie(id)
	}, func(db *gorm.DB, m *tmdb.Movie) error {
		_, err := IngestMovie(db, m)
		return err
	})
}

// syncShows processes the TV show change feed.
func syncShows(client *tmdb.Client, db *gorm.DB, since string) error {
	return syncChangeFeed(client, db, since, "tv", func(c *tmdb.Client, id int) (*tmdb.Show, error) {
		return c.GetShow(id)
	}, func(db *gorm.DB, s *tmdb.Show) error {
		_, err := IngestShow(db, s)
		if err != nil {
			return err
		}
		// For shows, also refresh seasons/episodes.
		// We skip full season refresh during sync to save API calls —
		// changed seasons appear in their own change feed entries.
		return nil
	})
}

// syncPeople processes the person change feed.
func syncPeople(client *tmdb.Client, db *gorm.DB, since string) error {
	return syncChangeFeed(client, db, since, "person", func(c *tmdb.Client, id int) (*tmdb.Person, error) {
		return c.GetPerson(id)
	}, func(db *gorm.DB, p *tmdb.Person) error {
		_, err := IngestPerson(db, p)
		return err
	})
}

// fetchFunc is a generic function to fetch a full detail record by ID.
type fetchFunc[T any] func(client *tmdb.Client, id int) (*T, error)

// ingestFunc is a generic function to ingest a record into the database.
type ingestFunc[T any] func(db *gorm.DB, item *T) error

// changeFeedFetcher fetches a page of change feed entries.
type changeFeedFetcher func(client *tmdb.Client, page int, since string) (*tmdb.ChangeFeed[tmdb.ChangeEntry], error)

// syncChangeFeed is a generic change feed processor.
func syncChangeFeed[T any](
	client *tmdb.Client,
	db *gorm.DB,
	since string,
	mediaType string,
	fetch fetchFunc[T],
	ingest ingestFunc[T],
) error {
	var fetcher changeFeedFetcher
	switch mediaType {
	case "movie":
		fetcher = func(c *tmdb.Client, page int, since string) (*tmdb.ChangeFeed[tmdb.ChangeEntry], error) {
			return c.GetMovieChanges(page, since)
		}
	case "tv":
		fetcher = func(c *tmdb.Client, page int, since string) (*tmdb.ChangeFeed[tmdb.ChangeEntry], error) {
			return c.GetShowChanges(page, since)
		}
	case "person":
		fetcher = func(c *tmdb.Client, page int, since string) (*tmdb.ChangeFeed[tmdb.ChangeEntry], error) {
			return c.GetPersonChanges(page, since)
		}
	default:
		return fmt.Errorf("unknown media type: %s", mediaType)
	}

	page := 1
	total := 0
	skipped := 0

	for {
		feed, err := fetcher(client, page, since)
		if err != nil {
			return fmt.Errorf("fetch %s changes page %d: %w", mediaType, page, err)
		}

		slog.Info("[sync] processing changes",
			"type", mediaType, "page", page,
			"total_pages", feed.TotalPages, "items", len(feed.Results))

		for _, entry := range feed.Results {
			item, err := fetch(client, entry.ID)
			if err != nil {
				slog.Error("[sync] fetch failed, skipping",
					"type", mediaType, "tmdb_id", entry.ID, "error", err)
				skipped++
				continue
			}

			if err := ingest(db, item); err != nil {
				slog.Error("[sync] ingest failed, skipping",
					"type", mediaType, "tmdb_id", entry.ID, "error", err)
				skipped++
				continue
			}
			total++

			// Rate limit: 250ms between API calls.
			time.Sleep(250 * time.Millisecond)
		}

		if page >= feed.TotalPages {
			break
		}
		page++
	}

	slog.Info("[sync] feed complete",
		"type", mediaType, "ingested", total, "skipped", skipped)
	return nil
}
