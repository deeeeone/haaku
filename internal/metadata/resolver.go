package metadata

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/external/tvdb"
	"github.com/haaku-tv/haaku/internal/database"

	"gorm.io/gorm"
)

// Sentinel errors the resolve path reports, so a handler can map "the provider
// says this does not exist" (404) apart from "the provider could not be asked"
// (502) without reading strings.
var (
	// ErrMediaNotFound is the provider's own answer that the requested record
	// does not exist (or that it carries none of the requested type).
	ErrMediaNotFound = errors.New("metadata: media not found")
	// ErrProviderUnavailable means the request could not be made: no TMDB
	// client is configured, or the provider failed/timed out.
	ErrProviderUnavailable = errors.New("metadata: metadata provider unavailable")
)

// Resolver turns an external id into a local media row, ingesting it from TMDB
// (with the TVDB supplement) the first time a title is asked for. It is the
// seam the search page uses: search results come from TMDB live, and opening or
// watchlisting one goes through here, so the local catalogue fills itself with
// the titles the owner actually cares about instead of a nightly guess at what
// is popular.
type Resolver struct {
	DB   *gorm.DB
	TMDB *tmdb.Client
	TVDB *tvdb.Client
}

// NewResolver builds a resolver. The TMDB client is the PRIMARY provider and is
// required to resolve anything; a nil TVDB client is allowed, and the
// supplement simply stays empty (the tvdb id link, the certification and
// trailer fallbacks and the raw TVDB record are then absent, and TMDB's own
// values stand).
func NewResolver(db *gorm.DB, tmdbClient *tmdb.Client, tvdbClient *tvdb.Client) *Resolver {
	return &Resolver{DB: db, TMDB: tmdbClient, TVDB: tvdbClient}
}

// Ready reports whether the resolver can reach its primary metadata provider.
func (r *Resolver) Ready() bool {
	return r != nil && r.DB != nil && r.TMDB != nil
}

// ResolveMovieByTMDBID returns the local movie behind a TMDB movie id,
// ingesting it when it is not in the catalogue yet. A row that already carries
// the TMDB id is returned without any provider call.
func (r *Resolver) ResolveMovieByTMDBID(ctx context.Context, tmdbID int) (*database.Movie, error) {
	if !r.Ready() {
		return nil, ErrProviderUnavailable
	}
	if tmdbID <= 0 {
		return nil, fmt.Errorf("%w: a tmdb movie id is required", ErrMediaNotFound)
	}

	if movie, err := r.localMovieByTMDBID(tmdbID); err != nil {
		return nil, err
	} else if movie != nil {
		return movie, nil
	}

	record, err := r.TMDB.GetMovie(tmdbID)
	if err != nil {
		return nil, wrapProviderError("tmdb movie", tmdbID, err)
	}

	return IngestMovie(r.DB, record, r.movieSupplement(ctx, record))
}

// ResolveShowByTMDBID returns the local show behind a TMDB TV id, ingesting it
// (with its seasons and episodes) when it is not in the catalogue yet.
func (r *Resolver) ResolveShowByTMDBID(ctx context.Context, tmdbID int) (*database.Show, error) {
	if !r.Ready() {
		return nil, ErrProviderUnavailable
	}
	if tmdbID <= 0 {
		return nil, fmt.Errorf("%w: a tmdb show id is required", ErrMediaNotFound)
	}

	if show, err := r.localShowByTMDBID(tmdbID); err != nil {
		return nil, err
	} else if show != nil {
		return show, nil
	}

	record, err := r.TMDB.GetShow(tmdbID)
	if err != nil {
		return nil, wrapProviderError("tmdb show", tmdbID, err)
	}

	return IngestShow(r.DB, record, r.seasons(ctx, record), r.showSupplement(ctx, record))
}

// ResolveByExternalID resolves a movie or show from any supported external id
// (tmdb, tvdb, imdb) to a canonical local row. mediaType is required for a TMDB
// id (the id only names a row inside a table); for a TVDB/IMDb id it is only a
// filter — TMDB's /find reports which types carry the id.
func (r *Resolver) ResolveByExternalID(ctx context.Context, mediaType, idType, externalID string) (string, int64, error) {
	if !r.Ready() {
		return "", 0, ErrProviderUnavailable
	}
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return "", 0, fmt.Errorf("%w: an external id is required", ErrMediaNotFound)
	}
	if idType == "" {
		idType = "tmdb"
	}

	// Already in the catalogue? Nothing to ingest, and no provider call.
	if mt, id, err := r.localByExternalID(mediaType, idType, externalID); err != nil {
		return "", 0, err
	} else if id != 0 {
		return mt, id, nil
	}

	tmdbID, resolvedType, err := r.tmdbIDFor(mediaType, idType, externalID)
	if err != nil {
		return "", 0, err
	}

	switch resolvedType {
	case "movie":
		movie, err := r.ResolveMovieByTMDBID(ctx, tmdbID)
		if err != nil {
			return "", 0, err
		}
		return "movie", movie.ID, nil
	default:
		show, err := r.ResolveShowByTMDBID(ctx, tmdbID)
		if err != nil {
			return "", 0, err
		}
		return "show", show.ID, nil
	}
}

// tmdbIDFor finds the TMDB id (and media type) an external id belongs to. A
// TMDB id is taken as given; anything else goes through TMDB's /find, which is
// TMDB's own answer about which record carries that id.
func (r *Resolver) tmdbIDFor(mediaType, idType, externalID string) (int, string, error) {
	if idType == "tmdb" {
		id, err := strconv.Atoi(externalID)
		if err != nil || id <= 0 {
			return 0, "", fmt.Errorf("%w: %q is not a tmdb id", ErrMediaNotFound, externalID)
		}
		if mediaType != "movie" && mediaType != "show" {
			return 0, "", fmt.Errorf("%w: type must be movie or show when resolving a tmdb id", ErrMediaNotFound)
		}
		return id, mediaType, nil
	}

	source := ""
	switch idType {
	case "imdb":
		source = "imdb_id"
	case "tvdb":
		source = "tvdb_id"
	default:
		return 0, "", fmt.Errorf("%w: id_type must be one of: tmdb, tvdb, imdb", ErrMediaNotFound)
	}
	// /find carries no context: it is one fast request, and the caller's
	// cancellation is observed by the detail fetch that follows.
	results, err := r.TMDB.Find(externalID, source)
	if err != nil {
		return 0, "", wrapProviderError("tmdb find", 0, err)
	}

	wantMovie := mediaType != "show"
	wantShow := mediaType != "movie"
	if wantMovie && len(results.MovieResults) > 0 {
		return results.MovieResults[0].ID, "movie", nil
	}
	if wantShow && len(results.TVResults) > 0 {
		return results.TVResults[0].ID, "show", nil
	}
	return 0, "", fmt.Errorf("%w: no tmdb record carries %s:%s", ErrMediaNotFound, idType, externalID)
}

// --- provider calls ---

// seasons walks a show's season list and fetches each season's episodes. A
// season that cannot be fetched is logged and skipped rather than failing the
// resolve: the show's own record is the valuable half, and the ingest derives
// its episode and season counts from the seasons it actually received, so a
// skipped season shows up honestly as a lower count instead of a lie.
func (r *Resolver) seasons(ctx context.Context, record *tmdb.Show) []tmdb.Season {
	if record == nil || len(record.Seasons) == 0 {
		return nil
	}
	seasons := make([]tmdb.Season, 0, len(record.Seasons))
	for _, summary := range record.Seasons {
		if err := ctx.Err(); err != nil {
			break
		}
		season, err := r.TMDB.GetSeason(record.ID, summary.SeasonNumber)
		if err != nil {
			slog.Debug("[resolve] tmdb season fetch failed",
				"tmdb_id", record.ID, "season", summary.SeasonNumber, "error", err)
			continue
		}
		seasons = append(seasons, *season)
	}
	return seasons
}

// movieSupplement fetches the TVDB record that supplements a TMDB movie, or nil
// when TVDB is not configured or does not carry the title.
//
// TMDB's own movie external-ids payload names no TVDB id (measured: GET
// /movie/603/external_ids returns imdb_id and the social handles only), so the
// link is recovered from the IMDb id through TVDB's remote-id search. A failure
// anywhere here is a debug line, never a failure: the supplement is optional by
// definition.
func (r *Resolver) movieSupplement(ctx context.Context, record *tmdb.Movie) *tvdb.MovieExtended {
	if r.TVDB == nil || record == nil {
		return nil
	}

	tvdbID := record.TVDBID()
	if tvdbID == 0 && record.IMDBID != "" {
		tvdbID = r.tvdbIDFor(ctx, record.IMDBID, "movie")
	}
	if tvdbID == 0 {
		return nil
	}

	movie, err := r.TVDB.GetMovieExtended(ctx, tvdbID)
	if err != nil {
		slog.Debug("[resolve] tvdb movie fetch failed", "tvdb_id", tvdbID, "error", err)
		return nil
	}
	return movie
}

// showSupplement is movieSupplement for a series. A show's link is cheaper: the
// TV external-ids payload TMDB appends names the tvdb id outright.
func (r *Resolver) showSupplement(ctx context.Context, record *tmdb.Show) *tvdb.SeriesExtended {
	if r.TVDB == nil || record == nil {
		return nil
	}

	tvdbID := record.TVDBID()
	if tvdbID == 0 && record.IMDBID != "" {
		tvdbID = r.tvdbIDFor(ctx, record.IMDBID, "show")
	}
	if tvdbID == 0 {
		return nil
	}

	show, err := r.TVDB.GetSeriesExtended(ctx, tvdbID)
	if err != nil {
		slog.Debug("[resolve] tvdb series fetch failed", "tvdb_id", tvdbID, "error", err)
		return nil
	}
	return show
}

// tvdbIDFor asks TVDB which of its records carries an id (the IMDb id, and
// TVDB's remote-id search indexes a TMDB id too), restricted to the media type
// the caller expects. One id can match several records — TMDB numbers movies
// and series in one space — so the type decides.
func (r *Resolver) tvdbIDFor(ctx context.Context, remoteID, mediaType string) int {
	matches, err := r.TVDB.SearchByRemoteID(ctx, remoteID)
	if err != nil {
		slog.Debug("[resolve] tvdb remote-id search failed", "id", remoteID, "error", err)
		return 0
	}
	for _, match := range matches {
		if match.ID > 0 && match.Type == mediaType {
			return match.ID
		}
	}
	return 0
}

// --- local lookups ---

func (r *Resolver) localMovieByTMDBID(tmdbID int) (*database.Movie, error) {
	var movie database.Movie
	err := r.DB.Where("tmdb_id = ?", tmdbID).First(&movie).Error
	if err == nil {
		return &movie, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("lookup movie by tmdb id: %w", err)
	}
	// A row ingested before the tmdb_id column existed is still findable
	// through its external_ids entry.
	mt, id, err := r.localByExternalID("movie", "tmdb", strconv.Itoa(tmdbID))
	if err != nil {
		return nil, err
	}
	if mt != "movie" || id == 0 {
		return nil, nil
	}
	if err := r.DB.Where("id = ?", id).First(&movie).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("lookup movie %d: %w", id, err)
	}
	return &movie, nil
}

func (r *Resolver) localShowByTMDBID(tmdbID int) (*database.Show, error) {
	var show database.Show
	err := r.DB.Where("tmdb_id = ?", tmdbID).First(&show).Error
	if err == nil {
		return &show, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("lookup show by tmdb id: %w", err)
	}
	mt, id, err := r.localByExternalID("show", "tmdb", strconv.Itoa(tmdbID))
	if err != nil {
		return nil, err
	}
	if mt != "show" || id == 0 {
		return nil, nil
	}
	if err := r.DB.Where("id = ?", id).First(&show).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("lookup show %d: %w", id, err)
	}
	return &show, nil
}

// localByExternalID resolves an external id through external_ids, restricted to
// the requested media type (or to movie/show when none was given). It returns
// ("", 0, nil) when nothing matches.
func (r *Resolver) localByExternalID(mediaType, idType, externalID string) (string, int64, error) {
	query := r.DB.Model(&database.ExternalID{}).Where("source = ? AND external_id = ?", idType, externalID)
	if mediaType != "" {
		query = query.Where("media_type = ?", mediaType)
	} else {
		query = query.Where("media_type IN ?", []string{"movie", "show"})
	}

	var ext database.ExternalID
	err := query.First(&ext).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, fmt.Errorf("lookup external id %s:%s: %w", idType, externalID, err)
	}

	// Confirm the media row still exists: external_ids has no FK, so a deleted
	// title can leave a stale lookup behind.
	switch ext.MediaType {
	case "movie":
		var count int64
		if err := r.DB.Model(&database.Movie{}).Where("id = ?", ext.MediaID).Count(&count).Error; err != nil {
			return "", 0, fmt.Errorf("verify movie %d: %w", ext.MediaID, err)
		}
		if count == 0 {
			return "", 0, nil
		}
	case "show":
		var count int64
		if err := r.DB.Model(&database.Show{}).Where("id = ?", ext.MediaID).Count(&count).Error; err != nil {
			return "", 0, fmt.Errorf("verify show %d: %w", ext.MediaID, err)
		}
		if count == 0 {
			return "", 0, nil
		}
	default:
		return "", 0, nil
	}

	return ext.MediaType, ext.MediaID, nil
}

// wrapProviderError maps a provider failure onto the resolver's sentinels: the
// provider's 404 is "not found", everything else is "could not be asked".
func wrapProviderError(what string, id int, err error) error {
	if err == nil {
		return nil
	}
	if tmdb.IsNotFound(err) || tvdb.IsNotFound(err) {
		return fmt.Errorf("%w: %s %d: %v", ErrMediaNotFound, what, id, err)
	}
	return fmt.Errorf("%w: %s %d: %v", ErrProviderUnavailable, what, id, err)
}
