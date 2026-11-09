//go:build postgres

// The real-PostgreSQL half of the catalogue-filter applier.
//
// sqlite proves the predicates are portable, but jsonb is not a sqlite type and
// the genre membership relies on the column's JSON array text. This test runs
// the same generated clauses against a real server, inside one pinned
// connection with temporary tables, so it proves the SQL the handlers will send
// in production without touching the migrated schema.

package shared

import (
	"os"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresCatalogueFilterSQL(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; the postgres suite runs via make test-db")
	}
	db, err := gorm.Open(postgres.Open(url), &gorm.Config{})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}

	// The scratch schema is dropped afterwards whether the assertions pass or
	// fail; the transaction's commit decides only whether it is visible to other
	// sessions for the duration of the run.
	defer func() { _ = db.Exec("DROP SCHEMA IF EXISTS pf_catalogue CASCADE").Error }()
	err = db.Transaction(func(tx *gorm.DB) error {
		for _, ddl := range []string{
			`DROP SCHEMA IF EXISTS pf_catalogue CASCADE`,
			`CREATE SCHEMA pf_catalogue`,
			`SET LOCAL search_path TO pf_catalogue`,
			`CREATE TABLE movies (id BIGINT, title TEXT, genres JSONB, released DATE,
				runtime INT, rating_avg DOUBLE PRECISION, status TEXT, certification TEXT)`,
			`CREATE TABLE shows (id BIGINT, title TEXT, genres JSONB, first_aired DATE,
				rating_avg DOUBLE PRECISION, status TEXT, certification TEXT)`,
			`CREATE TABLE episodes (id BIGINT, show_id BIGINT, runtime INT, aired_at DATE)`,
			`CREATE TABLE user_history (id BIGINT, user_id TEXT, media_type TEXT,
				media_id BIGINT, episode_id BIGINT, watched_at TIMESTAMPTZ)`,
		} {
			if err := tx.Exec(ddl).Error; err != nil {
				return err
			}
		}
		seeds := []string{
			`INSERT INTO movies VALUES
			 (1,'Both','["Action","Drama"]','2011-01-01',100,8.0,'released','R'),
			 (2,'Comedy','["Comedy"]','2021-01-01',90,4.0,'released',NULL),
			 (3,'Adventure','["Action & Adventure"]','2015-01-01',120,6.5,'released',NULL),
			 (4,'SciFi','["Science Fiction"]','2018-01-01',130,7.5,'released',NULL)`,
			`INSERT INTO shows VALUES
			 (10,'Show','["Drama"]','2015-01-01',7.0,'ended','TV-14')`,
			`INSERT INTO episodes VALUES (100,10,42,'2016-05-01')`,
			`INSERT INTO user_history VALUES
			 (1,'u','movie',1,NULL,'2026-01-01'),
			 (2,'u','movie',2,NULL,'2026-01-02'),
			 (3,'u','episode',100,100,'2026-01-03')`,
		}
		for _, s := range seeds {
			if err := tx.Exec(s).Error; err != nil {
				return err
			}
		}

		count := func(model, alias string, w where) int64 {
			var n int64
			table := model
			if alias != "" {
				table += " " + alias
			}
			if err := tx.Table(table).Where(w.clause, w.args...).Count(&n).Error; err != nil {
				t.Fatalf("%s count: %v", model, err)
			}
			return n
		}

		// A movie with two matching genres counts once, not twice.
		f := &CatalogueFilter{genres: []string{"action", "drama"}}
		if n := count("movies", "m", at(f.MovieWhere("m"))); n != 1 {
			t.Errorf("genre no-fanout: counted %d, want 1", n)
		}
		// "action" must not match "Action & Adventure".
		if n := count("movies", "m", at((&CatalogueFilter{genres: []string{"action"}}).MovieWhere("m"))); n != 1 {
			t.Errorf("action matched %d movies, want 1", n)
		}
		// Trakt's slug vocabulary maps onto TMDB's display name.
		if n := count("movies", "m", at((&CatalogueFilter{genres: []string{"science-fiction"}}).MovieWhere("m"))); n != 1 {
			t.Errorf("science-fiction matched %d movies, want 1", n)
		}
		// Years are a date range over released.
		if n := count("movies", "m", at((&CatalogueFilter{years: []span{{2010, 2012}}}).MovieWhere("m"))); n != 1 {
			t.Errorf("years matched %d movies, want 1", n)
		}
		// Ratings are stored 0-10, filtered 0-100.
		if n := count("movies", "m", at((&CatalogueFilter{ratings: []span{{70, 100}}}).MovieWhere("m"))); n != 2 {
			t.Errorf("ratings matched %d movies, want 2", n)
		}
		// Episode runtime plus its parent show's genre.
		ep := &CatalogueFilter{genres: []string{"drama"}, runtimes: []span{{40, 45}}}
		if n := count("episodes", "e", at(ep.EpisodeWhere("e"))); n != 1 {
			t.Errorf("episode filter matched %d, want 1", n)
		}
		// The history predicate handles both branches: the Drama movie and the
		// episode whose parent show is Drama, and drops the Comedy movie.
		hist := &CatalogueFilter{genres: []string{"drama"}}
		if n := count("user_history", "", at(hist.HistoryMediaWhere(tx))); n != 2 {
			t.Errorf("history genre matched %d rows, want 2", n)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("postgres catalogue filter run: %v", err)
	}
}

type where struct {
	clause string
	args   []any
}

func at(clause string, args []any) where { return where{clause, args} }
