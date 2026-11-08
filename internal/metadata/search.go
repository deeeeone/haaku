package metadata

import (
	"context"
	"errors"
	"log/slog"

	"github.com/haaku-tv/haaku/internal/database"
)

// ProviderHit is one search result from a live metadata provider (TVDB), in the
// shape the API layer renders: everything needed to show a card, plus whether
// the local catalogue already holds the title.
type ProviderHit struct {
	// MediaType is haaku's media type: "movie" or "show".
	MediaType string
	TVDBID    int
	TMDBID    int
	IMDBID    string
	Slug      string
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

// SearchProvider asks TVDB's name search (the PRIMARY source) for a query and
// annotates the hits with local-catalogue membership.
//
// The catalogue check is what lets the UI decide between "open this title" and
// "resolve this title first", and it is deliberately part of this call rather
// than a second round trip per result.
//
// A provider failure is returned to the caller to decide on: the search endpoint
// degrades to catalogue-only results rather than failing the page.
func (r *Resolver) SearchProvider(ctx context.Context, query string, mediaTypes []string, page, limit int) ([]ProviderHit, int, error) {
	if !r.Ready() {
		return nil, 0, ErrProviderUnavailable
	}

	response, err := r.TVDB.Search(ctx, query, mediaTypes, page, limit)
	if err != nil {
		return nil, 0, wrapProviderError("tvdb search", 0, err)
	}

	hits := make([]ProviderHit, 0, len(response.Data))
	for _, result := range response.Data {
		mediaType := result.MediaType()
		if mediaType == "" {
			// People/lists: TVDB search returns them but haaku has nothing to
			// ingest for them yet, and a card with no destination is worse
			// than no card.
			continue
		}
		if result.TVDBID() <= 0 {
			continue
		}
		hits = append(hits, ProviderHit{
			MediaType: mediaType,
			TVDBID:    result.TVDBID(),
			TMDBID:    result.TMDBID(),
			IMDBID:    result.IMDBID(),
			Slug:      result.Slug,
			Title:     result.Name,
			Year:      result.Year(),
			Overview:  result.Overview,
			PosterURL: result.ImageURL,
		})
	}

	if err := r.annotateCatalogMembership(hits); err != nil {
		// Membership is an annotation: failing to read it must not throw away
		// the provider results, so it degrades to "unknown" (all false) and
		// the UI resolves on click like it does for a miss.
		slog.Warn("[search] catalogue membership lookup failed", "error", err)
	}

	return hits, response.Links.TotalItems, nil
}

// annotateCatalogMembership marks each hit that the catalogue already holds,
// with two queries per media type at most.
func (r *Resolver) annotateCatalogMembership(hits []ProviderHit) error {
	byType := map[string][]int{}
	for _, hit := range hits {
		byType[hit.MediaType] = append(byType[hit.MediaType], hit.TVDBID)
	}

	lookup := make(map[string]map[int]int64, len(byType))
	for mediaType, ids := range byType {
		found, err := database.LookupMediaByTVDBIDs(r.DB, mediaType, ids)
		if err != nil {
			return err
		}
		lookup[mediaType] = found
	}

	for i := range hits {
		if id, ok := lookup[hits[i].MediaType][hits[i].TVDBID]; ok {
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
// knows but TVDB's search does not cover (person, list, episode) come back
// empty and must not fall through to a provider call.
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
