package tests

import (
	"testing"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/metadata"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupTestDB creates an in-memory SQLite database with the media schema
// for unit testing the record-level ingest functions (season, episode, person,
// credit). The composite movie/show ingest's own suite is provider_ingest_test.go.
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test DB: %v", err)
	}
	if err := db.AutoMigrate(database.MediaModels()...); err != nil {
		t.Fatalf("failed to migrate test DB: %v", err)
	}
	return db
}

// --- Movie Ingest Tests ---

func TestIngestMovie_InsertNew(t *testing.T) {
	db := setupTestDB(t)

	movie := &tmdb.Movie{
		ID:            12345,
		IMDBID:        "tt1234567",
		Title:         "Test Movie",
		OriginalTitle: "Test Movie Original",
		Tagline:       "A test tagline",
		Overview:      "This is a test movie.",
		ReleaseDate:   "2026-06-15",
		Runtime:       120,
		Status:        "Released",
		Popularity:    85.5,
		VoteAverage:   7.8,
		VoteCount:     1500,
		Genres: []tmdb.Genre{
			{ID: 28, Name: "Action"},
			{ID: 12, Name: "Adventure"},
		},
		Language:     "en",
		PosterPath:   "/poster123.jpg",
		BackdropPath: "/backdrop123.jpg",
		Adult:        false,
	}

	result, err := metadata.IngestMovie(db, movie, nil)
	if err != nil {
		t.Fatalf("IngestMovie failed: %v", err)
	}

	if result.ID == 0 {
		t.Error("expected non-zero ID after insert")
	}
	if result.TmdbID != 12345 {
		t.Errorf("expected TmdbID 12345, got %d", result.TmdbID)
	}
	if result.Title != "Test Movie" {
		t.Errorf("expected Title 'Test Movie', got %q", result.Title)
	}
	if result.Genres == nil || len(result.Genres) != 2 {
		t.Errorf("expected 2 genres, got %v", result.Genres)
	}
	if result.Images["poster_path"] != "/poster123.jpg" {
		t.Errorf("expected poster_path '/poster123.jpg', got %v", result.Images["poster_path"])
	}
	if result.Metadata == nil {
		t.Error("expected non-nil metadata JSONB")
	}
	if result.LastSyncedAt == nil {
		t.Error("expected non-nil LastSyncedAt")
	}

	// Verify it was persisted.
	var count int64
	db.Model(&database.Movie{}).Where("tmdb_id = ?", 12345).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 movie in DB, got %d", count)
	}
}

func TestIngestMovie_UpdateExisting(t *testing.T) {
	db := setupTestDB(t)

	// Insert initial movie.
	movie1 := &tmdb.Movie{
		ID:          12346,
		Title:       "Original Title",
		Overview:    "Original overview.",
		Popularity:  50.0,
		VoteAverage: 6.0,
		VoteCount:   500,
		Status:      "Released",
		Runtime:     90,
		Genres:      []tmdb.Genre{},
		Language:    "en",
		PosterPath:  "/old.jpg",
	}
	result1, err := metadata.IngestMovie(db, movie1, nil)
	if err != nil {
		t.Fatalf("first IngestMovie failed: %v", err)
	}
	originalID := result1.ID

	// Update with changed data.
	movie2 := &tmdb.Movie{
		ID:          12346,
		Title:       "Updated Title",
		Overview:    "Updated overview.",
		Popularity:  95.0,
		VoteAverage: 8.5,
		VoteCount:   2000,
		Status:      "Released",
		Runtime:     90,
		Genres:      []tmdb.Genre{{ID: 35, Name: "Comedy"}},
		Language:    "en",
		PosterPath:  "/new.jpg",
	}
	result2, err := metadata.IngestMovie(db, movie2, nil)
	if err != nil {
		t.Fatalf("second IngestMovie failed: %v", err)
	}

	// Should have the same ID (updated, not inserted).
	if result2.ID != originalID {
		t.Errorf("expected same ID %d, got %d", originalID, result2.ID)
	}
	if result2.Title != "Updated Title" {
		t.Errorf("expected 'Updated Title', got %q", result2.Title)
	}

	// Should still only be one row.
	var count int64
	db.Model(&database.Movie{}).Where("tmdb_id = ?", 12346).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 movie, got %d", count)
	}
}

func TestIngestMovie_UnchangedNoOp(t *testing.T) {
	db := setupTestDB(t)

	movie := &tmdb.Movie{
		ID:          12347,
		Title:       "Same Movie",
		Overview:    "Same overview.",
		Popularity:  70.0,
		VoteAverage: 7.0,
		VoteCount:   800,
		Status:      "Released",
		Runtime:     110,
		Genres:      []tmdb.Genre{},
		Language:    "en",
		PosterPath:  "/same.jpg",
	}

	result1, err := metadata.IngestMovie(db, movie, nil)
	if err != nil {
		t.Fatalf("first IngestMovie failed: %v", err)
	}

	result2, err := metadata.IngestMovie(db, movie, nil)
	if err != nil {
		t.Fatalf("second IngestMovie failed: %v", err)
	}

	if result2.ID != result1.ID {
		t.Errorf("expected same ID %d, got %d", result1.ID, result2.ID)
	}

	var count int64
	db.Model(&database.Movie{}).Where("tmdb_id = ?", 12347).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 movie, got %d", count)
	}
}

// --- Show Ingest Tests ---

func TestIngestShow_InsertNew(t *testing.T) {
	db := setupTestDB(t)

	show := &tmdb.Show{
		ID:            200,
		IMDBID:        "tt2000000",
		Name:          "Test Show",
		OriginalName:  "Test Show Original",
		Overview:      "A test TV show.",
		FirstAirDate:  "2025-01-10",
		LastAirDate:   "2026-05-20",
		Status:        "Returning Series",
		Popularity:    90.0,
		VoteAverage:   8.2,
		VoteCount:     3000,
		Genres:        []tmdb.Genre{{ID: 18, Name: "Drama"}},
		Language:      "en",
		PosterPath:    "/show_poster.jpg",
		BackdropPath:  "/show_backdrop.jpg",
		Networks:      []tmdb.Network{{ID: 1, Name: "HBO"}},
		SeasonsCount:  3,
		EpisodesCount: 30,
		Seasons: []tmdb.SeasonSummary{
			{ID: 2001, SeasonNumber: 1, EpisodeCount: 10, AirDate: "2025-01-10", PosterPath: "/s1.jpg"},
			{ID: 2002, SeasonNumber: 2, EpisodeCount: 10, AirDate: "2025-09-15", PosterPath: "/s2.jpg"},
		},
	}

	result, err := metadata.IngestShow(db, show, nil, nil)
	if err != nil {
		t.Fatalf("IngestShow failed: %v", err)
	}

	if result.TmdbID != 200 {
		t.Errorf("expected TmdbID 200, got %d", result.TmdbID)
	}
	if result.Title != "Test Show" {
		t.Errorf("expected Title 'Test Show', got %q", result.Title)
	}
	if result.Network != "HBO" {
		t.Errorf("expected Network 'HBO', got %q", result.Network)
	}
	if result.SeasonCount != 3 {
		t.Errorf("expected SeasonCount 3, got %d", result.SeasonCount)
	}

	var count int64
	db.Model(&database.Show{}).Where("tmdb_id = ?", 200).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 show, got %d", count)
	}
}

func TestIngestShow_UpdateExisting(t *testing.T) {
	db := setupTestDB(t)

	show1 := &tmdb.Show{
		ID:            201,
		Name:          "Old Show",
		Overview:      "Old overview.",
		Status:        "Ended",
		Popularity:    40.0,
		VoteAverage:   6.0,
		VoteCount:     400,
		SeasonsCount:  1,
		EpisodesCount: 10,
		Genres:        []tmdb.Genre{},
		Language:      "en",
		Networks:      []tmdb.Network{{Name: "NBC"}},
	}

	result1, err := metadata.IngestShow(db, show1, nil, nil)
	if err != nil {
		t.Fatalf("first IngestShow failed: %v", err)
	}

	show2 := &tmdb.Show{
		ID:            201,
		Name:          "Updated Show",
		Overview:      "Updated overview.",
		Status:        "Returning Series",
		Popularity:    85.0,
		VoteAverage:   9.0,
		VoteCount:     5000,
		SeasonsCount:  2,
		EpisodesCount: 20,
		Genres:        []tmdb.Genre{{Name: "Sci-Fi"}},
		Language:      "en",
		Networks:      []tmdb.Network{{Name: "Netflix"}},
	}

	result2, err := metadata.IngestShow(db, show2, nil, nil)
	if err != nil {
		t.Fatalf("second IngestShow failed: %v", err)
	}

	if result2.ID != result1.ID {
		t.Errorf("expected same ID %d, got %d", result1.ID, result2.ID)
	}
	if result2.Title != "Updated Show" {
		t.Errorf("expected 'Updated Show', got %q", result2.Title)
	}
	if result2.Network != "Netflix" {
		t.Errorf("expected Network 'Netflix', got %q", result2.Network)
	}

	var count int64
	db.Model(&database.Show{}).Where("tmdb_id = ?", 201).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 show, got %d", count)
	}
}

func TestIngestShow_UnchangedNoOp(t *testing.T) {
	db := setupTestDB(t)

	show := &tmdb.Show{
		ID:            202,
		Name:          "Static Show",
		Overview:      "Static overview.",
		Status:        "Ended",
		Popularity:    55.0,
		VoteAverage:   7.5,
		VoteCount:     600,
		SeasonsCount:  1,
		EpisodesCount: 8,
		Genres:        []tmdb.Genre{},
		Language:      "en",
		Networks:      []tmdb.Network{},
	}

	result1, err := metadata.IngestShow(db, show, nil, nil)
	if err != nil {
		t.Fatalf("first IngestShow failed: %v", err)
	}

	result2, err := metadata.IngestShow(db, show, nil, nil)
	if err != nil {
		t.Fatalf("second IngestShow failed: %v", err)
	}

	if result2.ID != result1.ID {
		t.Errorf("expected same ID %d, got %d", result1.ID, result2.ID)
	}

	var count int64
	db.Model(&database.Show{}).Where("tmdb_id = ?", 202).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 show, got %d", count)
	}
}

// --- Season Ingest Tests ---

func TestIngestSeason_InsertNew(t *testing.T) {
	db := setupTestDB(t)

	// First insert a show.
	show := &tmdb.Show{
		ID:            300,
		Name:          "Seasoned Show",
		Status:        "Returning Series",
		SeasonsCount:  2,
		EpisodesCount: 20,
		Popularity:    80.0,
		VoteAverage:   8.0,
		VoteCount:     1000,
		Genres:        []tmdb.Genre{},
		Language:      "en",
		Seasons: []tmdb.SeasonSummary{
			{SeasonNumber: 1, EpisodeCount: 10, AirDate: "2026-01-01"},
		},
	}
	dbShow, err := metadata.IngestShow(db, show, nil, nil)
	if err != nil {
		t.Fatalf("IngestShow failed: %v", err)
	}

	summary := tmdb.SeasonSummary{
		SeasonNumber: 1,
		EpisodeCount: 10,
		AirDate:      "2026-01-01",
		PosterPath:   "/season1.jpg",
	}

	season, err := metadata.IngestSeason(db, dbShow.ID, 300, summary)
	if err != nil {
		t.Fatalf("IngestSeason failed: %v", err)
	}

	if season.ShowID != dbShow.ID {
		t.Errorf("expected ShowID %d, got %d", dbShow.ID, season.ShowID)
	}
	if season.SeasonNumber != 1 {
		t.Errorf("expected SeasonNumber 1, got %d", season.SeasonNumber)
	}
	if season.EpisodeCount != 10 {
		t.Errorf("expected EpisodeCount 10, got %d", season.EpisodeCount)
	}

	var count int64
	db.Model(&database.Season{}).Where("show_id = ? AND season_number = ?", dbShow.ID, 1).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 season, got %d", count)
	}
}

func TestIngestSeason_UpdateExisting(t *testing.T) {
	db := setupTestDB(t)

	show := &tmdb.Show{
		ID:            301,
		Name:          "Season Update Show",
		Status:        "Returning Series",
		SeasonsCount:  1,
		EpisodesCount: 5,
		Popularity:    70.0,
		VoteAverage:   7.0,
		VoteCount:     500,
		Genres:        []tmdb.Genre{},
		Language:      "en",
	}
	dbShow, err := metadata.IngestShow(db, show, nil, nil)
	if err != nil {
		t.Fatalf("IngestShow failed: %v", err)
	}

	// First insert.
	s1 := tmdb.SeasonSummary{SeasonNumber: 1, EpisodeCount: 5, AirDate: "2026-01-01"}
	season1, err := metadata.IngestSeason(db, dbShow.ID, 301, s1)
	if err != nil {
		t.Fatalf("first IngestSeason failed: %v", err)
	}

	// Update with more episodes.
	s2 := tmdb.SeasonSummary{SeasonNumber: 1, EpisodeCount: 12, AirDate: "2026-01-01"}
	season2, err := metadata.IngestSeason(db, dbShow.ID, 301, s2)
	if err != nil {
		t.Fatalf("second IngestSeason failed: %v", err)
	}

	if season2.ID != season1.ID {
		t.Errorf("expected same ID %d, got %d", season1.ID, season2.ID)
	}
	if season2.EpisodeCount != 12 {
		t.Errorf("expected EpisodeCount 12, got %d", season2.EpisodeCount)
	}

	var count int64
	db.Model(&database.Season{}).Where("show_id = ? AND season_number = ?", dbShow.ID, 1).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 season, got %d", count)
	}
}

// --- Episode Ingest Tests ---

func TestIngestEpisode_InsertNew(t *testing.T) {
	db := setupTestDB(t)

	show := &tmdb.Show{
		ID:            400,
		Name:          "Episode Show",
		Status:        "Returning Series",
		SeasonsCount:  1,
		EpisodesCount: 1,
		Popularity:    60.0,
		VoteAverage:   7.0,
		VoteCount:     300,
		Genres:        []tmdb.Genre{},
		Language:      "en",
	}
	dbShow, err := metadata.IngestShow(db, show, nil, nil)
	if err != nil {
		t.Fatalf("IngestShow failed: %v", err)
	}

	summary := tmdb.SeasonSummary{SeasonNumber: 1, EpisodeCount: 1}
	dbSeason, err := metadata.IngestSeason(db, dbShow.ID, 400, summary)
	if err != nil {
		t.Fatalf("IngestSeason failed: %v", err)
	}

	ep := tmdb.EpisodeDetail{
		EpisodeNumber: 1,
		Name:          "Pilot",
		Overview:      "The first episode.",
		AirDate:       "2026-01-01",
		Runtime:       45,
		StillPath:     "/still1.jpg",
		SeasonNumber:  1,
		ShowID:        400,
	}

	result, err := metadata.IngestEpisode(db, dbSeason.ID, dbShow.ID, ep)
	if err != nil {
		t.Fatalf("IngestEpisode failed: %v", err)
	}

	if result.Title != "Pilot" {
		t.Errorf("expected Title 'Pilot', got %q", result.Title)
	}
	if result.EpisodeNumber != 1 {
		t.Errorf("expected EpisodeNumber 1, got %d", result.EpisodeNumber)
	}

	var count int64
	db.Model(&database.Episode{}).Where("show_id = ? AND season_id = ? AND episode_number = ?",
		dbShow.ID, dbSeason.ID, 1).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 episode, got %d", count)
	}
}

func TestIngestEpisode_UpdateExisting(t *testing.T) {
	db := setupTestDB(t)

	show := &tmdb.Show{
		ID:            401,
		Name:          "Episode Update Show",
		Status:        "Returning Series",
		SeasonsCount:  1,
		EpisodesCount: 1,
		Popularity:    50.0,
		VoteAverage:   6.0,
		VoteCount:     200,
		Genres:        []tmdb.Genre{},
		Language:      "en",
	}
	dbShow, _ := metadata.IngestShow(db, show, nil, nil)
	summary := tmdb.SeasonSummary{SeasonNumber: 1, EpisodeCount: 1}
	dbSeason, _ := metadata.IngestSeason(db, dbShow.ID, 401, summary)

	ep1 := tmdb.EpisodeDetail{EpisodeNumber: 1, Name: "Old Title", Overview: "Old.", Runtime: 30, SeasonNumber: 1, ShowID: 401}
	result1, err := metadata.IngestEpisode(db, dbSeason.ID, dbShow.ID, ep1)
	if err != nil {
		t.Fatalf("first IngestEpisode failed: %v", err)
	}

	ep2 := tmdb.EpisodeDetail{EpisodeNumber: 1, Name: "New Title", Overview: "Updated.", Runtime: 45, SeasonNumber: 1, ShowID: 401}
	result2, err := metadata.IngestEpisode(db, dbSeason.ID, dbShow.ID, ep2)
	if err != nil {
		t.Fatalf("second IngestEpisode failed: %v", err)
	}

	if result2.ID != result1.ID {
		t.Errorf("expected same ID %d, got %d", result1.ID, result2.ID)
	}
	if result2.Title != "New Title" {
		t.Errorf("expected 'New Title', got %q", result2.Title)
	}

	var count int64
	db.Model(&database.Episode{}).Where("show_id = ? AND season_id = ? AND episode_number = ?",
		dbShow.ID, dbSeason.ID, 1).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 episode, got %d", count)
	}
}

func TestIngestEpisode_UnchangedNoOp(t *testing.T) {
	db := setupTestDB(t)

	show := &tmdb.Show{
		ID: 402, Name: "Unchanged Ep Show", Status: "Ended",
		SeasonsCount: 1, EpisodesCount: 1, Popularity: 40.0, VoteAverage: 5.0, VoteCount: 100,
		Genres: []tmdb.Genre{}, Language: "en",
	}
	dbShow, _ := metadata.IngestShow(db, show, nil, nil)
	summary := tmdb.SeasonSummary{SeasonNumber: 1, EpisodeCount: 1}
	dbSeason, _ := metadata.IngestSeason(db, dbShow.ID, 402, summary)

	ep := tmdb.EpisodeDetail{EpisodeNumber: 1, Name: "Static Ep", Overview: "Static.", Runtime: 30, SeasonNumber: 1, ShowID: 402}
	result1, err := metadata.IngestEpisode(db, dbSeason.ID, dbShow.ID, ep)
	if err != nil {
		t.Fatalf("first IngestEpisode failed: %v", err)
	}

	result2, err := metadata.IngestEpisode(db, dbSeason.ID, dbShow.ID, ep)
	if err != nil {
		t.Fatalf("second IngestEpisode failed: %v", err)
	}

	if result2.ID != result1.ID {
		t.Errorf("expected same ID %d, got %d", result1.ID, result2.ID)
	}

	var count int64
	db.Model(&database.Episode{}).Where("show_id = ? AND season_id = ? AND episode_number = ?",
		dbShow.ID, dbSeason.ID, 1).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 episode, got %d", count)
	}
}

// --- Person Ingest Tests ---

func TestIngestPerson_InsertNew(t *testing.T) {
	db := setupTestDB(t)

	person := &tmdb.Person{
		ID:                 500,
		IMDBID:             "nm0000500",
		Name:               "Test Actor",
		Biography:          "A talented actor.",
		Birthday:           "1980-05-15",
		Gender:             2,
		KnownForDepartment: "Acting",
		ProfilePath:        "/profile500.jpg",
		Adult:              false,
	}

	result, err := metadata.IngestPerson(db, person)
	if err != nil {
		t.Fatalf("IngestPerson failed: %v", err)
	}

	if result.TmdbID != 500 {
		t.Errorf("expected TmdbID 500, got %d", result.TmdbID)
	}
	if result.Name != "Test Actor" {
		t.Errorf("expected Name 'Test Actor', got %q", result.Name)
	}
	if result.Gender != 2 {
		t.Errorf("expected Gender 2, got %d", result.Gender)
	}

	var count int64
	db.Model(&database.Person{}).Where("tmdb_id = ?", 500).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 person, got %d", count)
	}
}

func TestIngestPerson_UpdateExisting(t *testing.T) {
	db := setupTestDB(t)

	p1 := &tmdb.Person{ID: 501, Name: "Old Name", Biography: "Old bio.", Gender: 1}
	result1, err := metadata.IngestPerson(db, p1)
	if err != nil {
		t.Fatalf("first IngestPerson failed: %v", err)
	}

	p2 := &tmdb.Person{ID: 501, Name: "New Name", Biography: "Updated bio.", Gender: 2, KnownForDepartment: "Directing"}
	result2, err := metadata.IngestPerson(db, p2)
	if err != nil {
		t.Fatalf("second IngestPerson failed: %v", err)
	}

	if result2.ID != result1.ID {
		t.Errorf("expected same ID %d, got %d", result1.ID, result2.ID)
	}
	if result2.Name != "New Name" {
		t.Errorf("expected 'New Name', got %q", result2.Name)
	}
	if result2.KnownForDepartment != "Directing" {
		t.Errorf("expected 'Directing', got %q", result2.KnownForDepartment)
	}

	var count int64
	db.Model(&database.Person{}).Where("tmdb_id = ?", 501).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 person, got %d", count)
	}
}

func TestIngestPerson_UnchangedNoOp(t *testing.T) {
	db := setupTestDB(t)

	p := &tmdb.Person{ID: 502, Name: "Static Actor", Biography: "Static bio.", Gender: 1}
	result1, err := metadata.IngestPerson(db, p)
	if err != nil {
		t.Fatalf("first IngestPerson failed: %v", err)
	}

	result2, err := metadata.IngestPerson(db, p)
	if err != nil {
		t.Fatalf("second IngestPerson failed: %v", err)
	}

	if result2.ID != result1.ID {
		t.Errorf("expected same ID %d, got %d", result1.ID, result2.ID)
	}

	var count int64
	db.Model(&database.Person{}).Where("tmdb_id = ?", 502).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 person, got %d", count)
	}
}

// --- Batch Episode Tests ---

func TestIngestEpisodesBatch_InsertNew(t *testing.T) {
	db := setupTestDB(t)

	eps := []tmdb.EpisodeDetail{
		{EpisodeNumber: 1, Name: "Ep 1", Overview: "First.", Runtime: 42, SeasonNumber: 1, ShowID: 600},
		{EpisodeNumber: 2, Name: "Ep 2", Overview: "Second.", Runtime: 43, SeasonNumber: 1, ShowID: 600},
		{EpisodeNumber: 3, Name: "Ep 3", Overview: "Third.", Runtime: 44, SeasonNumber: 1, ShowID: 600},
	}

	err := metadata.IngestEpisodesBatch(db, 1, 1, eps)
	if err != nil {
		t.Fatalf("IngestEpisodesBatch failed: %v", err)
	}

	var count int64
	db.Model(&database.Episode{}).Where("show_id = ? AND season_id = ?", 1, 1).Count(&count)
	if count != 3 {
		t.Errorf("expected 3 episodes, got %d", count)
	}
}

func TestIngestEpisodesBatch_Upsert(t *testing.T) {
	db := setupTestDB(t)

	eps1 := []tmdb.EpisodeDetail{
		{EpisodeNumber: 1, Name: "Pilot", Overview: "First.", Runtime: 40, SeasonNumber: 1, ShowID: 601},
		{EpisodeNumber: 2, Name: "Second", Overview: "Second.", Runtime: 41, SeasonNumber: 1, ShowID: 601},
	}
	if err := metadata.IngestEpisodesBatch(db, 1, 1, eps1); err != nil {
		t.Fatalf("first batch insert failed: %v", err)
	}

	// Upsert with changed data.
	eps2 := []tmdb.EpisodeDetail{
		{EpisodeNumber: 1, Name: "Pilot (Updated)", Overview: "Updated first.", Runtime: 42, SeasonNumber: 1, ShowID: 601},
		{EpisodeNumber: 2, Name: "Second", Overview: "Second.", Runtime: 41, SeasonNumber: 1, ShowID: 601},
		{EpisodeNumber: 3, Name: "Third", Overview: "Third.", Runtime: 43, SeasonNumber: 1, ShowID: 601},
	}
	if err := metadata.IngestEpisodesBatch(db, 1, 1, eps2); err != nil {
		t.Fatalf("second batch upset failed: %v", err)
	}

	var count int64
	db.Model(&database.Episode{}).Where("show_id = ? AND season_id = ?", 1, 1).Count(&count)
	if count != 3 {
		t.Errorf("expected 3 episodes, got %d", count)
	}

	// Verify the updated title.
	var ep database.Episode
	db.Where("show_id = ? AND season_id = ? AND episode_number = ?", 1, 1, 1).First(&ep)
	if ep.Title != "Pilot (Updated)" {
		t.Errorf("expected 'Pilot (Updated)', got %q", ep.Title)
	}
}

// --- Credits Tests ---

func TestIngestCredits_InsertNew(t *testing.T) {
	db := setupTestDB(t)

	credits := &tmdb.CreditsResponse{
		ID: 700,
		Cast: []tmdb.CastMember{
			{ID: 701, Name: "Actor One", Character: "Hero", Order: 0},
			{ID: 702, Name: "Actor Two", Character: "Villain", Order: 1},
		},
		Crew: []tmdb.CrewMember{
			{ID: 703, Name: "Director One", Job: "Director", Department: "Directing"},
		},
	}

	err := metadata.IngestCredits(db, "movie", 1, credits)
	if err != nil {
		t.Fatalf("IngestCredits failed: %v", err)
	}

	var count int64
	db.Model(&database.Credit{}).Where("media_type = ? AND media_id = ?", "movie", 1).Count(&count)
	if count != 3 {
		t.Errorf("expected 3 credits, got %d", count)
	}
}

func TestIngestCredits_ReplaceExisting(t *testing.T) {
	db := setupTestDB(t)

	credits1 := &tmdb.CreditsResponse{
		ID: 800,
		Cast: []tmdb.CastMember{
			{ID: 801, Name: "Actor A", Character: "Role A", Order: 0},
		},
	}
	if err := metadata.IngestCredits(db, "movie", 2, credits1); err != nil {
		t.Fatalf("first IngestCredits failed: %v", err)
	}

	credits2 := &tmdb.CreditsResponse{
		ID: 800,
		Cast: []tmdb.CastMember{
			{ID: 802, Name: "Actor B", Character: "Role B", Order: 0},
			{ID: 803, Name: "Actor C", Character: "Role C", Order: 1},
		},
	}
	if err := metadata.IngestCredits(db, "movie", 2, credits2); err != nil {
		t.Fatalf("second IngestCredits failed: %v", err)
	}

	var count int64
	db.Model(&database.Credit{}).Where("media_type = ? AND media_id = ?", "movie", 2).Count(&count)
	if count != 2 {
		t.Errorf("expected 2 credits after replace, got %d", count)
	}
}

// --- Since File Tests ---

func TestLoadSaveSince(t *testing.T) {
	tmpDir := t.TempDir()
	sinceFile := tmpDir + "/since.txt"

	// Load non-existent file.
	s, err := metadata.LoadSince(sinceFile)
	if err != nil {
		t.Fatalf("LoadSince on missing file should not error: %v", err)
	}
	if s != "" {
		t.Errorf("expected empty string, got %q", s)
	}

	// Save a date.
	if err := metadata.SaveSince(sinceFile, "2026-08-01"); err != nil {
		t.Fatalf("SaveSince failed: %v", err)
	}

	// Load it back.
	s, err = metadata.LoadSince(sinceFile)
	if err != nil {
		t.Fatalf("LoadSince failed: %v", err)
	}
	if s != "2026-08-01" {
		t.Errorf("expected '2026-08-01', got %q", s)
	}
}
