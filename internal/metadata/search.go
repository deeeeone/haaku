package metadata

import (
	"context"
	"errors"
	"log/slog"
	"strconv"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/internal/database"
)

// ProviderHit is one search result from the live metadata provider (TMDB), in
// the shape the API layer renders: everything needed to show a card, plus
// whether the local catalogue already holds the title.
//
// A live hit carries only what TMDB's search endpoint returns — the TMDB id,
// the title, the year, the overview and a poster path. It has no slug, no IMDb
// id and no TVDB id, and claiming otherwise would mean four more API calls per
// result; the client resolves the hit (which fetches the full record) when the
// owner opens it.
type ProviderHit struct {
	// MediaType is haaku's media type: "movie" or "show".
	MediaType string
	TMDBID    int
	Title     string
	Year      int
	Overview  string
	PosterURL string

	// InCatalog is true when the local catalogue already has this title, in
	// which case MediaID is the row's id and the client may link straight to
	// it instead of resolving.
	InCatalog bool
	MediaID   int64
}

// SearchProvider asks TMDB's search (the PRIMARY source) for a query and
// annotates the hits with local-catalogue membership.
//
// The catalogue check is what lets the UI decide between "open this title" and
// "resolve this title first", and it is deliberately part of this call rather
// than a second round trip per result.
//
// A provider failure is returned to the caller to decide on: the search endpoint
// degrades to catalogue-only results rather than failing the page.
//
// TVDB is not searched. It is the SUPPLEMENTARY provider, and a TVDB-only record
// has no destination any more: the catalogue identity is the tmdb id, so a title
// TMDB does not carry cannot be ingested at all. Showing a card that cannot be
// opened would be worse than showing nothing.
func (r *Resolver) SearchProvider(ctx context.Context, query string, mediaTypes []string, page, limit int) ([]ProviderHit, int, error) {
	if !r.Ready() {
		return nil, 0, ErrProviderUnavailable
	}
	_ = ctx // TMDB's search endpoints take no context; the HTTP client has its own timeout.

	results, total, err := r.searchTMDB(query, mediaTypes, page)
	if err != nil {
		return nil, 0, wrapProviderError("tmdb search", 0, err)
	}

	hits := make([]ProviderHit, 0, len(results))
	for _, result := range results {
		mediaType := providerMediaType(result.MediaType)
		if mediaType == "" {
			// TMDB's multi-search also returns people, which have nowhere to
			// go until issue #68 lands person ingestion. A card with no
			// destination is worse than no card.
			continue
		}
		if result.ID <= 0 {
			continue
		}
		hits = append(hits, ProviderHit{
			MediaType: mediaType,
			TMDBID:    result.ID,
			Title:     result.DisplayName(),
			Year:      yearOfTitle(result),
			Overview:  result.Overview,
			PosterURL: tmdb.ImageURL(result.PosterPath, tmdb.ImageSizePoster),
		})
	}

	if err := r.annotateCatalogMembership(hits); err != nil {
		// Membership is an annotation: failing to read it must not throw away
		// the provider results, so it degrades to "unknown" (all false) and
		// the UI resolves on click like it does for a miss.
		slog.Warn("[search] catalogue membership lookup failed", "error", err)
	}

	return hits, total, nil
}

// searchTMDB runs a query against the right TMDB search endpoint: the
// type-scoped one when the caller asked for a single type, /search/multi when
// it asked for both.
//
// The distinction matters for pagination rather than for correctness:
// /search/multi takes no type filter, so asking it for movies and dropping the
// series would hand the client a short page it was told was full.
func (r *Resolver) searchTMDB(query string, mediaTypes []string, page int) ([]tmdb.Title, int, error) {
	if len(mediaTypes) == 1 {
		switch mediaTypes[0] {
		case "movie":
			result, err := r.TMDB.SearchMovie(query, page)
			if err != nil {
				return nil, 0, err
			}
			return result.Results, result.TotalResults, nil
		case "show":
			result, err := r.TMDB.SearchTV(query, page)
			if err != nil {
				return nil, 0, err
			}
			return result.Results, result.TotalResults, nil
		}
	}

	result, err := r.TMDB.SearchMulti(query, page)
	if err != nil {
		return nil, 0, err
	}
	return result.Results, result.TotalResults, nil
}

// annotateCatalogMembership marks each hit that the catalogue already holds,
// with two queries per media type at most.
func (r *Resolver) annotateCatalogMembership(hits []ProviderHit) error {
	byType := map[string][]int{}
	for _, hit := range hits {
		byType[hit.MediaType] = append(byType[hit.MediaType], hit.TMDBID)
	}

	lookup := make(map[string]map[int]int64, len(byType))
	for mediaType, ids := range byType {
		found, err := database.LookupMediaByTMDBIDs(r.DB, mediaType, ids)
		if err != nil {
			return err
		}
		lookup[mediaType] = found
	}

	for i := range hits {
		if id, ok := lookup[hits[i].MediaType][hits[i].TMDBID]; ok {
			hits[i].InCatalog = true
			hits[i].MediaID = id
		}
	}
	return nil
}

// IsProviderNotFound reports whether err means "the provider says no such
// record" rather than "the provider could not be reached".
func IsProviderNotFound(err error) bool { return errors.Is(err, ErrMediaNotFound) }

// IsProviderUnavailable is IsProviderNotFound's counterpart.
func IsProviderUnavailable(err error) bool { return errors.Is(err, ErrProviderUnavailable) }

// NormalizeMediaTypes maps a caller's type filter onto the provider types, and
// reports whether the filter is one the provider can serve at all. Types haaku
// knows but TMDB's search is not wired for here (person, list, episode) come
// back empty and must not fall through to a provider call.
func NormalizeMediaTypes(types []string) (providerTypes []string, supported bool) {
	for _, t := range types {
		switch t {
		case "movie":
			providerTypes = append(providerTypes, "movie")
		case "show":
			providerTypes = append(providerTypes, "show")
		}
	}
	return providerTypes, len(providerTypes) > 0
}

// yearOfTitle returns a search hit's year, or 0 when TMDB did not supply one
// (a title with no release date yet).
func yearOfTitle(title tmdb.Title) int {
	year, err := strconv.Atoi(title.ReleaseYear())
	if err != nil {
		return 0
	}
	return year
}

// providerMediaType maps TMDB's vocabulary onto haaku's, or "" for a type haaku
// cannot ingest.
//
// The mapping is not cosmetic: /search/multi reports a series as "tv", while
// every haaku route, table and client speaks "show". Passing TMDB's word
// through drops every series from a search page while the movies still render —
// a page that looks like it works and silently holds half the catalogue.
func providerMediaType(mediaType string) string {
	switch mediaType {
	case "movie":
		return "movie"
	case "tv", "show":
		return "show"
	default:
		return ""
	}
}
