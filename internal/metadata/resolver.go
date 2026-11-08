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
	// ErrMediaNotFound is TVDB's own answer that the requested record does not
	// exist (or that it carries none of the requested type).
	ErrMediaNotFound = errors.New("metadata: media not found")
	// ErrProviderUnavailable means the request could not be made: no TVDB
	// client is configured, or the provider failed/timed out.
	ErrProviderUnavailable = errors.New("metadata: metadata provider unavailable")
)

// Resolver turns an external id into a local media row, ingesting from TVDB
// (with the TMDB supplement) the first time a title is asked for. It is the
// seam the search page uses: search results come from TVDB live, and opening or
// watchlisting one goes through here, so the local catalogue fills itself with
// the titles the owner actually cares about instead of a nightly guess at what
// is popular.
type Resolver struct {
	DB   *gorm.DB
	TVDB *tvdb.Client
	TMDB *tmdb.Client
}

// NewResolver builds a resolver. A nil TMDB client is allowed: TVDB alone is a
// complete record, and the supplement simply stays empty.
func NewResolver(db *gorm.DB, tvdbClient *tvdb.Client, tmdbClient *tmdb.Client) *Resolver {
	return &Resolver{DB: db, TVDB: tvdbClient, TMDB: tmdbClient}
}

// Ready reports whether the resolver can reach a metadata provider at all.
func (r *Resolver) Ready() bool {
	return r != nil && r.DB != nil && r.TVDB != nil
}

// ResolveMovieByTVDBID returns the local movie behind a TVDB movie id,
// ingesting it when it is not in the catalogue yet. A row that already carries
// the TVDB id is returned without any provider call.
func (r *Resolver) ResolveMovieByTVDBID(ctx context.Context, tvdbID int) (*database.Movie, error) {
	if !r.Ready() {
		return nil, ErrProviderUnavailable
	}
	if tvdbID <= 0 {
		return nil, fmt.Errorf("%w: a tvdb movie id is required", ErrMediaNotFound)
	}

	if movie, err := r.localMovieByTVDBID(tvdbID); err != nil {
		return nil, err
	} else if movie != nil {
		return movie, nil
	}

	record, err := r.TVDB.GetMovieExtended(ctx, tvdbID)
	if err != nil {
		return nil, wrapProviderError("tvdb movie", tvdbID, err)
	}

	supplement := r.movieSupplement(ctx, record)
	return IngestTVDBMovie(r.DB, record, supplement)
}

// ResolveShowByTVDBID returns the local show behind a TVDB series id,
// ingesting it (with its seasons and episodes) when it is not in the catalogue
// yet.
func (r *Resolver) ResolveShowByTVDBID(ctx context.Context, tvdbID int) (*database.Show, error) {
	if !r.Ready() {
		return nil, ErrProviderUnavailable
	}
	if tvdbID <= 0 {
		return nil, fmt.Errorf("%w: a tvdb series id is required", ErrMediaNotFound)
	}

	if show, err := r.localShowByTVDBID(tvdbID); err != nil {
		return nil, err
	} else if show != nil {
		return show, nil
	}

	record, err := r.TVDB.GetSeriesExtended(ctx, tvdbID)
	if err != nil {
		return nil, wrapProviderError("tvdb series", tvdbID, err)
	}
	episodes, err := r.TVDB.GetSeriesEpisodes(ctx, tvdbID, "", "")
	if err != nil {
		return nil, wrapProviderError("tvdb series episodes", tvdbID, err)
	}

	supplement := r.showSupplement(ctx, record)
	return IngestTVDBShow(r.DB, record, episodes, supplement)
}

// ResolveByExternalID resolves a movie or show from any supported external id
// (tvdb, tmdb, imdb) to a canonical local row. mediaType is required for a TVDB
// id (TVDB numbers series and movies in separate spaces, so the id alone does
// not say which endpoint to read); for a TMDB/IMDb id it is only a filter —
// TVDB's remote-id search reports the type.
func (r *Resolver) ResolveByExternalID(ctx context.Context, mediaType, idType, externalID string) (string, int64, error) {
	if !r.Ready() {
		return "", 0, ErrProviderUnavailable
	}
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return "", 0, fmt.Errorf("%w: an external id is required", ErrMediaNotFound)
	}
	if idType == "" {
		idType = "tvdb"
	}

	// Already in the catalogue? Nothing to ingest, and no provider call.
	if mt, id, err := r.localByExternalID(mediaType, idType, externalID); err != nil {
		return "", 0, err
	} else if id != 0 {
		return mt, id, nil
	}

	tvdbID, resolvedType, err := r.tvdbIDFor(ctx, mediaType, idType, externalID)
	if err != nil {
		return "", 0, err
	}

	switch resolvedType {
	case "movie":
		movie, err := r.ResolveMovieByTVDBID(ctx, tvdbID)
		if err != nil {
			return "", 0, err
		}
		return "movie", movie.ID, nil
	default:
		show, err := r.ResolveShowByTVDBID(ctx, tvdbID)
		if err != nil {
			return "", 0, err
		}
		return "show", show.ID, nil
	}
}

// tvdbIDFor finds the TVDB id (and media type) an external id belongs to. A
// TVDB id is taken as given; anything else goes through TVDB's remote-id
// search, which is TVDB's own answer about which record carries that id.
func (r *Resolver) tvdbIDFor(ctx context.Context, mediaType, idType, externalID string) (int, string, error) {
	if idType == "tvdb" {
		id, err := strconv.Atoi(externalID)
		if err != nil || id <= 0 {
			return 0, "", fmt.Errorf("%w: %q is not a tvdb id", ErrMediaNotFound, externalID)
		}
		if mediaType != "movie" && mediaType != "show" {
			return 0, "", fmt.Errorf("%w: type must be movie or show when resolving a tvdb id", ErrMediaNotFound)
		}
		return id, mediaType, nil
	}

	results, err := r.TVDB.SearchByRemoteID(ctx, externalID)
	if err != nil {
		return 0, "", wrapProviderError("tvdb remote id search", 0, err)
	}
	for _, hit := range results {
		hitType := hit.Type
		if hitType == "" {
			continue
		}
		if mediaType != "" && hitType != mediaType {
			continue
		}
		if hit.ID > 0 {
			return hit.ID, hitType, nil
		}
	}
	return 0, "", fmt.Errorf("%w: no tvdb record carries %s:%s", ErrMediaNotFound, idType, externalID)
}

// --- provider calls ---

// movieSupplement fetches the TMDB record that supplements a TVDB movie, or
// nil when TMDB cannot be asked or does not carry the title. TVDB's remote id
// is preferred; the reverse lookup is the fallback for a record whose TVDB
// links are missing or stale.
func (r *Resolver) movieSupplement(ctx context.Context, record *tvdb.MovieExtended) *tmdb.Movie {
	if r.TMDB == nil {
		return nil
	}

	tmdbID := record.TMDBID()
	if tmdbID == 0 {
		id, err := r.TMDB.MovieIDByTVDBID(record.ID)
		if err != nil {
			slog.Debug("[resolve] tmdb tvdb-id lookup failed", "tvdb_id", record.ID, "error", err)
			return nil
		}
		tmdbID = id
	}
	if tmdbID == 0 {
		return nil
	}

	movie, err := r.TMDB.GetMovie(tmdbID)
	if err != nil {
		slog.Debug("[resolve] tmdb movie fetch failed", "tmdb_id", tmdbID, "error", err)
		return nil
	}
	return movie
}

// showSupplement is movieSupplement for a series.
func (r *Resolver) showSupplement(ctx context.Context, record *tvdb.SeriesExtended) *tmdb.Show {
	if r.TMDB == nil {
		return nil
	}

	tmdbID := record.TMDBID()
	if tmdbID == 0 {
		id, err := r.TMDB.ShowIDByTVDBID(record.ID)
		if err != nil {
			slog.Debug("[resolve] tmdb tvdb-id lookup failed", "tvdb_id", record.ID, "error", err)
			return nil
		}
		tmdbID = id
	}
	if tmdbID == 0 {
		return nil
	}

	show, err := r.TMDB.GetShow(tmdbID)
	if err != nil {
		slog.Debug("[resolve] tmdb show fetch failed", "tmdb_id", tmdbID, "error", err)
		return nil
	}
	return show
}

// --- local lookups ---

func (r *Resolver) localMovieByTVDBID(tvdbID int) (*database.Movie, error) {
	var movie database.Movie
	err := r.DB.Where("tvdb_id = ?", tvdbID).First(&movie).Error
	if err == nil {
		return &movie, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("lookup movie by tvdb id: %w", err)
	}
	// A row ingested before the tvdb_id column existed is still findable
	// through its external_ids entry.
	mt, id, err := r.localByExternalID("movie", "tvdb", strconv.Itoa(tvdbID))
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

func (r *Resolver) localShowByTVDBID(tvdbID int) (*database.Show, error) {
	var show database.Show
	err := r.DB.Where("tvdb_id = ?", tvdbID).First(&show).Error
	if err == nil {
		return &show, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("lookup show by tvdb id: %w", err)
	}
	mt, id, err := r.localByExternalID("show", "tvdb", strconv.Itoa(tvdbID))
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

// wrapProviderError maps a provider failure onto the resolver's sentinels:
// TVDB's 404 is "not found", everything else is "could not be asked".
func wrapProviderError(what string, id int, err error) error {
	if err == nil {
		return nil
	}
	if tvdb.IsNotFound(err) {
		return fmt.Errorf("%w: %s %d: %v", ErrMediaNotFound, what, id, err)
	}
	return fmt.Errorf("%w: %s %d: %v", ErrProviderUnavailable, what, id, err)
}
