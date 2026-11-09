package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Fixtures are the ids the seeded rows landed on, written to disk so the
// harness can address them by value rather than by guess (bigserial ids depend
// on the migration order, which is not ours to assume).
type Fixtures struct {
	MovieID   int64  `json:"movie_id"`
	Movie2ID  int64  `json:"movie2_id"`
	MovieTMDB int    `json:"movie_tmdb"`
	ShowID    int64  `json:"show_id"`
	ShowTMDB  int    `json:"show_tmdb"`
	SeasonID  int64  `json:"season_id"`
	EpisodeID int64  `json:"episode_id"`
	PersonID  int64  `json:"person_id"`
	ListID    int64  `json:"list_id"`
	CommentID int64  `json:"comment_id"`
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
	Email     string `json:"email"`
	Password  string `json:"password"`
}

// seedFixtures inserts a small but complete catalogue and one viewer's data,
// idempotently, so the conformance run has real rows to serve. It never
// invents a payload: every case reads what is here.
func seedFixtures(db *gorm.DB, fx *Fixtures) error {
	now := time.Now().UTC()

	movie := database.Movie{
		TmdbID: 550, ImdbID: "tt0137523", Slug: "fight-club",
		Title: "Fight Club", OriginalTitle: "Fight Club",
		Overview: "An insomniac office worker looks for a way to change his life.",
		Genres:   database.StringSlice{"Drama"}, Language: "en", Status: "released",
		Popularity: 100, RatingAvg: 8.4, RatingCount: 40000,
		Images: database.JSONMap{"poster": []any{"https://example.test/fc.jpg"}, "fanart": []any{}},
	}
	if err := db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&movie).Error; err != nil {
		return fmt.Errorf("seed movie: %w", err)
	}
	movie2 := database.Movie{
		TmdbID: 680, ImdbID: "tt0110912", Slug: "pulp-fiction",
		Title: "Pulp Fiction", OriginalTitle: "Pulp Fiction",
		Genres: database.StringSlice{"Crime"}, Language: "en", Status: "released",
		Popularity: 90, RatingAvg: 8.9, RatingCount: 35000,
		Images: database.JSONMap{"poster": []any{}, "fanart": []any{}},
	}
	if err := db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&movie2).Error; err != nil {
		return fmt.Errorf("seed movie2: %w", err)
	}

	show := database.Show{
		TmdbID: 1396, ImdbID: "tt0903747", Slug: "breaking-bad",
		Title: "Breaking Bad", OriginalTitle: "Breaking Bad",
		Overview: "A chemistry teacher becomes a drug kingpin.",
		Network:  "AMC", Status: "ended", EpisodeCount: 2, SeasonCount: 1,
		Genres: database.StringSlice{"Drama"}, Language: "en", Popularity: 200, RatingAvg: 9.3,
		Images: database.JSONMap{"poster": []any{}, "fanart": []any{}},
	}
	if err := db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&show).Error; err != nil {
		return fmt.Errorf("seed show: %w", err)
	}

	season := database.Season{ShowID: show.ID, TmdbShowID: 1396, SeasonNumber: 1, EpisodeCount: 2}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&season).Error; err != nil {
		return fmt.Errorf("seed season: %w", err)
	}
	if season.ID == 0 { // existed already: read it back
		if err := db.Where("show_id = ? AND season_number = ?", show.ID, 1).First(&season).Error; err != nil {
			return err
		}
	}
	absOne, absTwo := 1, 2
	episodes := []database.Episode{
		{SeasonID: season.ID, ShowID: show.ID, EpisodeNumber: 1, Title: "Pilot",
			Runtime: 58, AbsoluteNumber: &absOne},
		{SeasonID: season.ID, ShowID: show.ID, EpisodeNumber: 2, Title: "Cat's in the Bag...",
			Runtime: 48, AbsoluteNumber: &absTwo},
	}
	for i := range episodes {
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&episodes[i]).Error; err != nil {
			return fmt.Errorf("seed episode: %w", err)
		}
	}

	person := database.Person{TmdbID: 17419, ImdbID: "nm0186505", Name: "Bryan Cranston", Gender: 2,
		KnownForDepartment: "Acting"}
	if err := db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&person).Error; err != nil {
		return fmt.Errorf("seed person: %w", err)
	}
	credit := database.Credit{PersonID: person.ID, MediaType: "show", MediaID: show.ID,
		CreditType: "cast", Character: "Walter White", Order: 0, SeriesRegular: true}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&credit).Error; err != nil {
		return fmt.Errorf("seed credit: %w", err)
	}

	ext := []database.ExternalID{
		{MediaType: "movie", MediaID: movie.ID, Source: "slug", ExternalID: "fight-club"},
		{MediaType: "movie", MediaID: movie.ID, Source: "tvdb", ExternalID: "1022"},
		{MediaType: "movie", MediaID: movie2.ID, Source: "slug", ExternalID: "pulp-fiction"},
		{MediaType: "show", MediaID: show.ID, Source: "slug", ExternalID: "breaking-bad"},
		{MediaType: "show", MediaID: show.ID, Source: "tvdb", ExternalID: "81189"},
	}
	for i := range ext {
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&ext[i]).Error; err != nil {
			return fmt.Errorf("seed external id: %w", err)
		}
	}

	// The viewer. A local account with a known password, so the harness logs in
	// over HTTP and gets a token the same way a client does.
	fx.UserID = "local_conformance"
	fx.Username = "conformance"
	fx.Email = "conformance@example.test"
	fx.Password = "conformance-password-123"
	user := database.User{ID: fx.UserID, Username: fx.Username, Email: fx.Email,
		Name: "Conformance", Status: database.UserStatusActive, Plan: "free"}
	if err := db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&user).Error; err != nil {
		return fmt.Errorf("seed user: %w", err)
	}
	hash, err := auth.HashPassword(fx.Password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	cred := database.LocalCredential{UserID: fx.UserID, PasswordHash: hash, PasswordUpdatedAt: now}
	if err := db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&cred).Error; err != nil {
		return fmt.Errorf("seed credential: %w", err)
	}

	uid := fx.UserID
	history := []database.UserHistory{
		{UserID: uid, MediaType: "movie", MediaID: movie.ID, WatchedAt: now.Add(-48 * time.Hour), Source: "api"},
		{UserID: uid, MediaType: "episode", MediaID: episodes[0].ID, SeasonID: &season.ID,
			EpisodeID: &episodes[0].ID, WatchedAt: now.Add(-24 * time.Hour), Source: "api"},
	}
	for i := range history {
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&history[i]).Error; err != nil {
			return fmt.Errorf("seed history: %w", err)
		}
	}
	collection := []database.UserCollection{
		{UserID: uid, MediaType: "movie", MediaID: movie.ID, CollectedAt: now.Add(-72 * time.Hour)},
		{UserID: uid, MediaType: "episode", MediaID: episodes[0].ID, CollectedAt: now.Add(-72 * time.Hour)},
	}
	for i := range collection {
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&collection[i]).Error; err != nil {
			return fmt.Errorf("seed collection: %w", err)
		}
	}
	watchlist := []database.UserWatchlist{
		{UserID: uid, MediaType: "movie", MediaID: movie2.ID, Rank: 1, ListedAt: now.Add(-12 * time.Hour)},
		{UserID: uid, MediaType: "show", MediaID: show.ID, Rank: 2, ListedAt: now.Add(-11 * time.Hour)},
	}
	for i := range watchlist {
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&watchlist[i]).Error; err != nil {
			return fmt.Errorf("seed watchlist: %w", err)
		}
	}
	ratings := []database.UserRating{
		{UserID: uid, MediaType: "movie", MediaID: movie.ID, Rating: 9, RatedAt: now.Add(-36 * time.Hour)},
		{UserID: uid, MediaType: "show", MediaID: show.ID, Rating: 10, RatedAt: now.Add(-35 * time.Hour)},
	}
	for i := range ratings {
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&ratings[i]).Error; err != nil {
			return fmt.Errorf("seed rating: %w", err)
		}
	}
	fav := database.UserFavorite{UserID: uid, MediaType: "movie", MediaID: movie.ID, Rank: 1,
		FavoritedAt: now.Add(-30 * time.Hour)}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&fav).Error; err != nil {
		return fmt.Errorf("seed favorite: %w", err)
	}

	// One paused playback row per type, so /sync/playback and its typed feeds
	// return a populated item rather than an empty array that validates nothing.
	playback := []database.UserPlayback{
		{UserID: uid, MediaType: "movie", MediaID: movie.ID, Progress: 42.5, PausedAt: now.Add(-2 * time.Hour)},
		{UserID: uid, MediaType: "episode", MediaID: episodes[0].ID, Progress: 17, PausedAt: now.Add(-3 * time.Hour)},
	}
	for i := range playback {
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&playback[i]).Error; err != nil {
			return fmt.Errorf("seed playback: %w", err)
		}
	}

	comment := database.Comment{UserID: uid, MediaType: "movie", MediaID: &movie.ID,
		CommentType: "shout", Body: "A conformance comment.", Spoiler: false}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&comment).Error; err != nil {
		return fmt.Errorf("seed comment: %w", err)
	}

	list := database.List{UserID: uid, Name: "Conformance list", Slug: "conformance-list",
		Privacy: "public", SortBy: "rank", SortHow: "asc", AllowComments: true}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&list).Error; err != nil {
		return fmt.Errorf("seed list: %w", err)
	}
	if list.ID == 0 {
		if err := db.Where("slug = ?", "conformance-list").First(&list).Error; err != nil {
			return err
		}
	}
	item := database.ListItem{ListID: list.ID, MediaType: "movie", MediaID: movie.ID, Rank: 1, AddedBy: uid, AddedAt: now}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&item).Error; err != nil {
		return fmt.Errorf("seed list item: %w", err)
	}

	fx.MovieID, fx.Movie2ID, fx.MovieTMDB = movie.ID, movie2.ID, movie.TmdbID
	fx.ShowID, fx.ShowTMDB = show.ID, show.TmdbID
	fx.SeasonID, fx.EpisodeID = season.ID, episodes[0].ID
	fx.PersonID = person.ID
	fx.ListID = list.ID
	fx.CommentID = comment.ID
	return nil
}

// writeFixtures persists the fixture ids for the harness process.
func writeFixtures(path string, fx Fixtures) error {
	b, err := json.MarshalIndent(fx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// loadFixtures reads the fixture ids written by -seed.
func loadFixtures(path string) (Fixtures, error) {
	var fx Fixtures
	b, err := os.ReadFile(path)
	if err != nil {
		return fx, fmt.Errorf("read fixtures %s (run `go run ./cmd/trakt-conformance -seed` first): %w", path, err)
	}
	if err := json.Unmarshal(b, &fx); err != nil {
		return fx, err
	}
	return fx, nil
}
