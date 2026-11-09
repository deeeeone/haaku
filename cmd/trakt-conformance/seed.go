package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
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
//
// Every id a case sends is read from here. A case never carries a literal id it
// hopes exists: change a seeded id and the case follows it (see
// TestCasesFollowFixtureIDs).
type Fixtures struct {
	MovieID   int64  `json:"movie_id"`
	Movie2ID  int64  `json:"movie2_id"`
	MovieTMDB int    `json:"movie_tmdb"`
	MovieIMDB string `json:"movie_imdb"`
	MovieSlug string `json:"movie_slug"`
	MovieTVDB int    `json:"movie_tvdb"`

	ShowID     int64  `json:"show_id"`
	ShowTMDB   int    `json:"show_tmdb"`
	ShowIMDB   string `json:"show_imdb"`
	ShowSlug   string `json:"show_slug"`
	ShowTVDB   int    `json:"show_tvdb"`
	SeasonID   int64  `json:"season_id"`
	SeasonNum  int    `json:"season_number"`
	EpisodeID  int64  `json:"episode_id"`
	Episode2ID int64  `json:"episode2_id"`
	EpisodeNum int    `json:"episode_number"`

	PersonID   int64  `json:"person_id"`
	PersonTMDB int    `json:"person_tmdb"`
	PersonIMDB string `json:"person_imdb"`
	PersonSlug string `json:"person_slug"`

	ListID      int64  `json:"list_id"`
	ListSlug    string `json:"list_slug"`
	ListItemID  int64  `json:"list_item_id"`
	SmartListID int64  `json:"smart_list_id"`
	CommentID   int64  `json:"comment_id"`
	// ThreadCommentID has a reply and a like, so the list-shaped assertions on
	// /comments/{id}/replies and /comments/{id}/likes validate a real row rather
	// than a vacuous empty array. CommentID itself stays reply-free so GET is
	// not affected by reply state, and DeleteCommentID is a third row so the
	// destructive DELETE case cannot consume the GET fixture.
	ThreadCommentID int64  `json:"thread_comment_id"`
	DeleteCommentID int64  `json:"delete_comment_id"`
	NoteID          int64  `json:"note_id"`
	DeleteNoteID    int64  `json:"delete_note_id"`
	UserID          string `json:"user_id"`
	Username        string `json:"username"`
	Email           string `json:"email"`
	Password        string `json:"password"`
	// TargetUserID is a second account, mutually followed by the viewer so the
	// follower/following/friends lists are non-empty. Follow/block cases use
	// FollowUserID/BlockUserID instead, so their destructive calls cannot empty
	// the read graph the list assertions check.
	TargetUserID   string `json:"target_user_id"`
	TargetUsername string `json:"target_username"`
	FollowUserID   string `json:"follow_user_id"`
	BlockUserID    string `json:"block_user_id"`
	// BlockedReadUserID exists only so GET /users/blocked has a row to return.
	// It is deliberately a different account from BlockUserID, whose block the
	// destructive block/unblock cases consume.
	BlockedReadUserID string `json:"blocked_read_user_id"`

	// OAuth application, seeded in the database. DeviceCode cannot be persisted
	// by the seed: the device grant store is process-local, so the harness
	// creates one at startup through the real /oauth/device/code endpoint and
	// authorizes it through /oauth/device/verify (see oauth.go).
	AppClientID     string `json:"app_client_id"`
	AppClientSecret string `json:"app_client_secret"`
	RedirectURI     string `json:"redirect_uri"`
	// GrantCode is a seeded authorization-code grant for the confidential
	// (authorization_code + PKCE) token exchange.
	GrantCode     string `json:"grant_code"`
	CodeVerifier  string `json:"code_verifier"`
	CodeChallenge string `json:"code_challenge"`
	DeviceCode    string `json:"device_code,omitempty"`
}

// firstOrCreate loads dst by its natural key when the row is already there and
// creates it when it is not.
//
// Neither upsert form works here. clause.OnConflict{UpdateAll: true} with no
// Columns targets the primary key, and these inserts carry ID 0, so
// "ON CONFLICT (id) DO UPDATE" never fires — the second -seed then inserted a
// fresh row and died on the natural key's unique index. Naming the column does
// not work either: the index behind it is partial, and PostgreSQL refuses to infer
// a partial index without its predicate ("there is no unique or exclusion
// constraint matching the ON CONFLICT specification", SQLSTATE 42P10). A read
// followed by a create says what it means on both PostgreSQL and sqlite.
func firstOrCreate(db *gorm.DB, dst any, where string, args ...any) error {
	err := db.Where(where, args...).First(dst).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return db.Create(dst).Error
}

// seedFixtures inserts a small but complete catalogue and one viewer's data, so
// the conformance run has real rows to serve. It never invents a payload: every
// case reads what is here.
//
// It is re-runnable: rows with a natural key upsert on that key, and rows
// inserted with DoNothing are read back when the insert returned no id, so the
// fixture ids written to disk are the same on every run.
func seedFixtures(db *gorm.DB, fx *Fixtures) error {
	now := time.Now().UTC()

	movie := database.Movie{
		TmdbID: 550, ImdbID: "tt0137523", Slug: "fight-club",
		Title: "Fight Club", OriginalTitle: "Fight Club",
		Overview: "An insomniac office worker looks for a way to change his life.",
		Genres:   database.StringSlice{"Drama"}, Language: "en", Status: "released",
		Popularity: 100, RatingAvg: 8.4, RatingCount: 40000,
		Released: &database.Date{Time: now},
		Images:   database.JSONMap{"poster": []any{"https://example.test/fc.jpg"}, "fanart": []any{}},
	}
	if err := firstOrCreate(db, &movie, "tmdb_id = ?", movie.TmdbID); err != nil {
		return fmt.Errorf("seed movie: %w", err)
	}
	movie2 := database.Movie{
		TmdbID: 680, ImdbID: "tt0110912", Slug: "pulp-fiction",
		Title: "Pulp Fiction", OriginalTitle: "Pulp Fiction",
		Genres: database.StringSlice{"Crime"}, Language: "en", Status: "released",
		Popularity: 90, RatingAvg: 8.9, RatingCount: 35000,
		Images: database.JSONMap{"poster": []any{}, "fanart": []any{}},
	}
	if err := firstOrCreate(db, &movie2, "tmdb_id = ?", movie2.TmdbID); err != nil {
		return fmt.Errorf("seed movie2: %w", err)
	}

	show := database.Show{
		TmdbID: 1396, ImdbID: "tt0903747", Slug: "breaking-bad",
		Title: "Breaking Bad", OriginalTitle: "Breaking Bad",
		Overview: "A chemistry teacher becomes a drug kingpin.",
		Network:  "AMC", Status: "ended", EpisodeCount: 3, SeasonCount: 2,
		Genres: database.StringSlice{"Drama"}, Language: "en", Popularity: 200, RatingAvg: 9.3,
		FirstAired: &database.Date{Time: now.AddDate(0, 0, -7)},
		Images:     database.JSONMap{"poster": []any{}, "fanart": []any{}},
	}
	if err := firstOrCreate(db, &show, "tmdb_id = ?", show.TmdbID); err != nil {
		return fmt.Errorf("seed show: %w", err)
	}

	network := database.Network{ID: 174, Name: "AMC"}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&network).Error; err != nil {
		return fmt.Errorf("seed network: %w", err)
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
			Runtime: 58, AbsoluteNumber: &absOne, AiredAt: &database.Date{Time: now}},
		{SeasonID: season.ID, ShowID: show.ID, EpisodeNumber: 2, Title: "Cat's in the Bag...",
			Runtime: 48, AbsoluteNumber: &absTwo, AiredAt: &database.Date{Time: now}},
	}
	for i := range episodes {
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&episodes[i]).Error; err != nil {
			return fmt.Errorf("seed episode: %w", err)
		}
		if episodes[i].ID == 0 { // existed already: read it back
			// DoNothing returns no row on conflict, so a second -seed run would
			// otherwise carry EpisodeID 0 into the history/collection/playback
			// rows below and address a media id that does not exist.
			if err := db.Where("season_id = ? AND episode_number = ?", season.ID, episodes[i].EpisodeNumber).
				First(&episodes[i]).Error; err != nil {
				return fmt.Errorf("seed episode read-back: %w", err)
			}
		}
	}

	// A second season with its own premiere, so the /calendars/releases/hot/
	// {premieres} assertion has a season_number > 1 row rather than an empty
	// array. Aired today so it falls inside the calendar window.
	season2 := database.Season{ShowID: show.ID, TmdbShowID: 1396, SeasonNumber: 2, EpisodeCount: 1}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&season2).Error; err != nil {
		return fmt.Errorf("seed season2: %w", err)
	}
	if season2.ID == 0 {
		if err := db.Where("show_id = ? AND season_number = ?", show.ID, 2).First(&season2).Error; err != nil {
			return fmt.Errorf("seed season2 read-back: %w", err)
		}
	}
	episode3 := database.Episode{SeasonID: season2.ID, ShowID: show.ID, EpisodeNumber: 1,
		Title: "Seven Thirty-Seven", AiredAt: &database.Date{Time: now}}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&episode3).Error; err != nil {
		return fmt.Errorf("seed episode3: %w", err)
	}

	person := database.Person{TmdbID: 17419, ImdbID: "nm0186505", Name: "Bryan Cranston", Gender: 2,
		KnownForDepartment: "Acting"}
	if err := firstOrCreate(db, &person, "tmdb_id = ?", person.TmdbID); err != nil {
		return fmt.Errorf("seed person: %w", err)
	}
	credit := database.Credit{PersonID: person.ID, MediaType: "show", MediaID: show.ID,
		CreditType: "cast", Character: "Walter White", Order: 0, SeriesRegular: true}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&credit).Error; err != nil {
		return fmt.Errorf("seed credit: %w", err)
	}

	// Every id flavour a case can send is present in external_ids, including
	// tmdb and imdb. The media resolver reads this table (not the movies.tmdb_id
	// column), so a body carrying ids.tmdb=550 resolved to nothing until these
	// rows existed — that was the "media not found" fixture gap.
	ext := []database.ExternalID{
		{MediaType: "movie", MediaID: movie.ID, Source: "slug", ExternalID: "fight-club"},
		{MediaType: "movie", MediaID: movie.ID, Source: "tmdb", ExternalID: "550"},
		{MediaType: "movie", MediaID: movie.ID, Source: "imdb", ExternalID: "tt0137523"},
		{MediaType: "movie", MediaID: movie.ID, Source: "tvdb", ExternalID: "1022"},
		{MediaType: "movie", MediaID: movie2.ID, Source: "slug", ExternalID: "pulp-fiction"},
		{MediaType: "movie", MediaID: movie2.ID, Source: "tmdb", ExternalID: "680"},
		{MediaType: "movie", MediaID: movie2.ID, Source: "imdb", ExternalID: "tt0110912"},
		{MediaType: "show", MediaID: show.ID, Source: "slug", ExternalID: "breaking-bad"},
		{MediaType: "show", MediaID: show.ID, Source: "tmdb", ExternalID: "1396"},
		{MediaType: "show", MediaID: show.ID, Source: "imdb", ExternalID: "tt0903747"},
		{MediaType: "show", MediaID: show.ID, Source: "tvdb", ExternalID: "81189"},
		{MediaType: "season", MediaID: season.ID, Source: "tmdb", ExternalID: "1396"},
		{MediaType: "episode", MediaID: episodes[0].ID, Source: "tmdb", ExternalID: "1396"},
		{MediaType: "person", MediaID: person.ID, Source: "tmdb", ExternalID: "17419"},
		{MediaType: "person", MediaID: person.ID, Source: "imdb", ExternalID: "nm0186505"},
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

	// A distinct target account for follow/block. The handlers refuse
	// targeting yourself, so this is not optional.
	fx.TargetUserID = "local_conformance_target"
	fx.TargetUsername = "conformance-target"
	target := database.User{ID: fx.TargetUserID, Username: fx.TargetUsername,
		Email: "conformance-target@example.test", Name: "Conformance Target",
		Status: database.UserStatusActive, Plan: "free"}
	if err := db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&target).Error; err != nil {
		return fmt.Errorf("seed target user: %w", err)
	}
	fx.FollowUserID = "local_conformance_follow"
	fx.BlockUserID = "local_conformance_block"
	for _, u := range []database.User{
		{ID: fx.FollowUserID, Username: "conformance-follow", Email: "conformance-follow@example.test",
			Name: "Conformance Follow", Status: database.UserStatusActive, Plan: "free"},
		{ID: fx.BlockUserID, Username: "conformance-block", Email: "conformance-block@example.test",
			Name: "Conformance Block", Status: database.UserStatusActive, Plan: "free"},
	} {
		if err := db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&u).Error; err != nil {
			return fmt.Errorf("seed user %s: %w", u.ID, err)
		}
	}
	// A mutual, approved follow between the viewer and the target, so
	// followers/following/friends are non-empty. The follow/block cases target
	// other accounts, so their destructive calls leave this graph intact.
	approved := now
	follows := []database.UserFollow{
		{FollowerID: fx.UserID, FollowingID: fx.TargetUserID, ApprovedAt: &approved, CreatedAt: now},
		{FollowerID: fx.TargetUserID, FollowingID: fx.UserID, ApprovedAt: &approved, CreatedAt: now},
		// A pre-existing follow the DELETE /users/{id}/follow case removes.
		{FollowerID: fx.UserID, FollowingID: fx.FollowUserID, ApprovedAt: &approved, CreatedAt: now},
	}
	for i := range follows {
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&follows[i]).Error; err != nil {
			return fmt.Errorf("seed follow: %w", err)
		}
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

	// Comments carry no unique constraint to conflict on, so DoNothing silently
	// appended a second row on every re-run and the fixture id moved. Read-or-create
	// on the natural key instead.
	comment := database.Comment{UserID: uid, MediaType: "movie", MediaID: &movie.ID,
		CommentType: "shout", Body: "A conformance comment.", Spoiler: false}
	if err := firstOrCreate(db, &comment, "user_id = ? AND media_type = ? AND media_id = ?",
		uid, "movie", movie.ID); err != nil {
		return fmt.Errorf("seed comment: %w", err)
	}
	thread := database.Comment{UserID: uid, MediaType: "show", MediaID: &show.ID,
		CommentType: "shout", Body: "A conformance comment with a reply.", Spoiler: false}
	if err := firstOrCreate(db, &thread, "user_id = ? AND media_type = ? AND media_id = ?",
		uid, "show", show.ID); err != nil {
		return fmt.Errorf("seed thread comment: %w", err)
	}
	reply := database.Comment{UserID: fx.TargetUserID, ParentID: &thread.ID, MediaType: "show",
		MediaID: &show.ID, CommentType: "shout", Body: "A conformance reply."}
	if err := firstOrCreate(db, &reply, "parent_id = ? AND user_id = ?", thread.ID, fx.TargetUserID); err != nil {
		return fmt.Errorf("seed reply: %w", err)
	}
	// A comment on the season, so /shows/{id}/seasons/{season}/comments/{sort}
	// is not an empty-array pass.
	seasonComment := database.Comment{UserID: fx.TargetUserID, MediaType: "season",
		MediaID: &season.ID, CommentType: "shout", Body: "A conformance season comment."}
	if err := firstOrCreate(db, &seasonComment, "user_id = ? AND media_type = ? AND media_id = ?",
		fx.TargetUserID, "season", season.ID); err != nil {
		return fmt.Errorf("seed season comment: %w", err)
	}
	// And an episode comment, for the episode-comments route.
	episodeComment := database.Comment{UserID: fx.TargetUserID, MediaType: "episode",
		MediaID: &episodes[0].ID, CommentType: "shout", Body: "A conformance episode comment."}
	if err := firstOrCreate(db, &episodeComment, "user_id = ? AND media_type = ? AND media_id = ?",
		fx.TargetUserID, "episode", episodes[0].ID); err != nil {
		return fmt.Errorf("seed episode comment: %w", err)
	}
	// A third comment the destructive DELETE case consumes, leaving CommentID
	// for the GET.
	deleteComment := database.Comment{UserID: uid, MediaType: "movie", MediaID: &movie2.ID,
		CommentType: "shout", Body: "A conformance comment to delete."}
	if err := firstOrCreate(db, &deleteComment, "user_id = ? AND media_type = ? AND media_id = ?",
		uid, "movie", movie2.ID); err != nil {
		return fmt.Errorf("seed delete comment: %w", err)
	}
	for _, like := range []database.CommentLike{
		{CommentID: thread.ID, UserID: uid, CreatedAt: now},
		{CommentID: comment.ID, UserID: fx.TargetUserID, CreatedAt: now},
	} {
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&like).Error; err != nil {
			return fmt.Errorf("seed comment like: %w", err)
		}
	}

	list := database.List{UserID: uid, Name: "Conformance list", Slug: "conformance-list",
		Privacy: "public", SortBy: "rank", SortHow: "asc", AllowComments: true}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&list).Error; err != nil {
		return fmt.Errorf("seed list: %w", err)
	}
	if list.ID == 0 { // existed already: read it back
		if err := db.Where("user_id = ? AND slug = ?", uid, "conformance-list").First(&list).Error; err != nil {
			return fmt.Errorf("seed list read-back: %w", err)
		}
	}
	item := database.ListItem{ListID: list.ID, MediaType: "movie", MediaID: movie.ID, Rank: 1, AddedBy: uid, AddedAt: now}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&item).Error; err != nil {
		return fmt.Errorf("seed list item: %w", err)
	}
	if item.ID == 0 {
		if err := db.Where("list_id = ? AND media_type = ? AND media_id = ?", list.ID, "movie", movie.ID).
			First(&item).Error; err != nil {
			return fmt.Errorf("seed list item read-back: %w", err)
		}
	}
	// An episode item too, so the episode-lists route has a row to serve.
	episodeItem := database.ListItem{ListID: list.ID, MediaType: "episode", MediaID: episodes[0].ID,
		Rank: 2, AddedBy: uid, AddedAt: now}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&episodeItem).Error; err != nil {
		return fmt.Errorf("seed episode list item: %w", err)
	}
	// The like is the target's, not the viewer's: the viewer's like/unlike
	// cases would otherwise empty /lists/{id}/likes before it is read.
	listLike := database.ListLike{ListID: list.ID, UserID: fx.TargetUserID, CreatedAt: now}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&listLike).Error; err != nil {
		return fmt.Errorf("seed list like: %w", err)
	}

	// A smart list and a private note, so /smart-lists/{id} and /notes/{id}
	// address real rows instead of measuring a 404.
	smart := database.SmartList{UserID: uid, Name: "Conformance smart list",
		Filters: database.JSONMap{"media_type": "movie"}}
	if err := firstOrCreate(db, &smart, "user_id = ? AND name = ?", uid, "Conformance smart list"); err != nil {
		return fmt.Errorf("seed smart list: %w", err)
	}
	note := database.UserNote{UserID: uid, MediaType: "movie", MediaID: movie.ID,
		Note: "A conformance note."}
	if err := firstOrCreate(db, &note, "user_id = ? AND media_type = ? AND media_id = ?",
		uid, "movie", movie.ID); err != nil {
		return fmt.Errorf("seed note: %w", err)
	}
	// A second note the DELETE case consumes.
	deleteNote := database.UserNote{UserID: uid, MediaType: "show", MediaID: show.ID,
		Note: "A conformance note to delete."}
	if err := firstOrCreate(db, &deleteNote, "user_id = ? AND media_type = ? AND media_id = ?",
		uid, "show", show.ID); err != nil {
		return fmt.Errorf("seed delete note: %w", err)
	}

	// A hidden row in the section the contract's enum puts first ("calendar"),
	// so GET /users/hidden/{section} is not an empty-array pass.
	hidden := database.UserHidden{UserID: uid, Section: "calendar", MediaType: "movie",
		MediaID: movie.ID, HiddenAt: now}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&hidden).Error; err != nil {
		return fmt.Errorf("seed hidden: %w", err)
	}

	// OAuth application. The client ID is a UUID (the handler queries the column
	// as uuid), so let PostgreSQL generate it on first insert and read it back on
	// re-runs by the unique name.
	fx.RedirectURI = "https://example.test/callback"
	fx.AppClientSecret = "conformance-client-secret"
	client := database.OAuthClient{Name: "conformance", UserID: uid,
		RedirectURIs: database.StringArray{fx.RedirectURI},
		SecretHash:   sha256Hex(fx.AppClientSecret)}
	if err := firstOrCreate(db, &client, "name = ?", client.Name); err != nil {
		return fmt.Errorf("seed oauth client: %w", err)
	}
	fx.AppClientID = client.ID

	fx.CodeVerifier = "conformance-code-verifier"
	sum := sha256.Sum256([]byte(fx.CodeVerifier))
	fx.CodeChallenge = base64.RawURLEncoding.EncodeToString(sum[:])
	fx.GrantCode = "conformance-grant-code"
	// Raw SQL, not db.Create: oauth_grants.scopes is TEXT[] in migration 0004
	// while database.OAuthGrant.Scopes is a JSON StringArray, so GORM's INSERT
	// sends a JSON array and PostgreSQL rejects it (22P02). The read path copes
	// (StringArray.Scan parses {profile}); only the write needs the array
	// literal. That model/migration mismatch is a handler-side defect, not
	// something this harness may fix.
	var grantID string
	if err := db.Raw("SELECT id FROM oauth_grants WHERE code_hash = ?", sha256Hex(fx.GrantCode)).
		Scan(&grantID).Error; err != nil {
		return fmt.Errorf("seed oauth grant read: %w", err)
	}
	if grantID == "" {
		err := db.Exec(`INSERT INTO oauth_grants
			(client_id, user_id, code_hash, code_challenge, code_challenge_method, redirect_uri, scopes, expires_at, used, created_at)
			VALUES (?, ?, ?, ?, 'S256', ?, '{profile}'::text[], ?, false, ?)`,
			client.ID, uid, sha256Hex(fx.GrantCode), fx.CodeChallenge, fx.RedirectURI,
			now.Add(24*time.Hour), now).Error
		if err != nil {
			return fmt.Errorf("seed oauth grant: %w", err)
		}
	} else {
		// Re-seeding must leave the grant usable: refresh the expiry without
		// changing the code_hash (so the fixture id set is identical).
		if err := db.Exec("UPDATE oauth_grants SET expires_at = ?, used = false WHERE id = ?",
			now.Add(24*time.Hour), grantID).Error; err != nil {
			return fmt.Errorf("seed oauth grant refresh: %w", err)
		}
	}

	// --- rows the profile list assertions need --------------------------------
	//
	// Five operations failed conformance with cause "empty" rather than a real
	// field diff: the seed never created the rows their list assertions read, so
	// an empty array validated against the schema and proved nothing (#123).
	// These are real rows for real reads, like the fixtures above.
	blockedRead := database.User{ID: "local_conformance_blocked_read",
		Username: "conformance-blocked-read", Email: "conformance-blocked-read@example.test",
		Name: "Conformance Blocked Read", Status: database.UserStatusActive, Plan: "free"}
	if err := db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&blockedRead).Error; err != nil {
		return fmt.Errorf("seed blocked-read user: %w", err)
	}
	fx.BlockedReadUserID = blockedRead.ID
	// The block the viewer owns, so GET /users/blocked is non-empty. BlockUserID
	// is a different account because the block/unblock cases consume that one.
	block := database.UserBlock{BlockerID: uid, BlockedID: blockedRead.ID, CreatedAt: now}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&block).Error; err != nil {
		return fmt.Errorf("seed user block: %w", err)
	}
	reaction := database.CommentReaction{CommentID: comment.ID, UserID: uid,
		ReactionType: "like", CreatedAt: now}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&reaction).Error; err != nil {
		return fmt.Errorf("seed comment reaction: %w", err)
	}
	// One saved filter per section the handler accepts, so whichever section the
	// generated case asks for there is a real row to assert on.
	for _, section := range []string{"movies", "shows", "calendars", "search"} {
		sf := database.SavedFilter{UserID: uid, Section: section, Name: "conformance-" + section,
			Filter: database.JSONMap{"genres": "drama"}, CreatedAt: now}
		if err := firstOrCreate(db, &sf, "user_id = ? AND section = ? AND name = ?",
			uid, section, sf.Name); err != nil {
			return fmt.Errorf("seed saved filter %s: %w", section, err)
		}
	}
	// The ratings slice above holds no episode row, so /users/{id}/ratings/episodes
	// had an empty list.
	episodeRating := database.UserRating{UserID: uid, MediaType: "episode",
		MediaID: episodes[0].ID, Rating: 8, RatedAt: now.Add(-30 * time.Hour)}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&episodeRating).Error; err != nil {
		return fmt.Errorf("seed episode rating: %w", err)
	}
	// /users/{id}/{type}/activities reads history inside a window starting at the
	// requested start_date, which the case sends as today, while every history row
	// above sits in the past - so the feed was legitimately empty rather than
	// wrong. One row watched today, for the viewer and for the followed target,
	// gives both the own feed and the friends feed something real.
	for _, actor := range []string{uid, fx.TargetUserID} {
		h := database.UserHistory{UserID: actor, MediaType: "movie", MediaID: movie.ID,
			WatchedAt: now, Source: "api"}
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&h).Error; err != nil {
			return fmt.Errorf("seed today history for %s: %w", actor, err)
		}
	}

	fx.MovieID, fx.Movie2ID, fx.MovieTMDB = movie.ID, movie2.ID, movie.TmdbID
	fx.MovieIMDB, fx.MovieSlug, fx.MovieTVDB = movie.ImdbID, movie.Slug, 1022
	fx.ShowID, fx.ShowTMDB = show.ID, show.TmdbID
	fx.ShowIMDB, fx.ShowSlug, fx.ShowTVDB = show.ImdbID, show.Slug, 81189
	fx.SeasonID, fx.SeasonNum = season.ID, season.SeasonNumber
	fx.EpisodeID, fx.Episode2ID, fx.EpisodeNum = episodes[0].ID, episodes[1].ID, episodes[0].EpisodeNumber
	fx.PersonID = person.ID
	fx.PersonTMDB, fx.PersonIMDB = person.TmdbID, person.ImdbID
	fx.PersonSlug = "bryan-cranston"
	fx.ListID, fx.ListSlug = list.ID, list.Slug
	fx.ListItemID = item.ID
	fx.SmartListID = smart.ID
	fx.CommentID = comment.ID
	fx.ThreadCommentID = thread.ID
	fx.DeleteCommentID = deleteComment.ID
	fx.NoteID = note.ID
	fx.DeleteNoteID = deleteNote.ID
	return nil
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
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
