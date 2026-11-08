package httpapi

import (
	"errors"
	"fmt"
	"sync"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/external/tvdb"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/metadata"
)

// The API holds ONE resolver per process: it owns the TMDB client (the PRIMARY
// metadata provider, and the one every resolve and search call actually waits
// on) and the TVDB client the supplement comes from. The clients are built
// lazily on first use rather than at startup so an API process that never
// searches costs nothing, and a failure is retried on the next call instead of
// being cached.
var (
	resolverMu     sync.Mutex
	resolverCached *metadata.Resolver
)

// metadataResolver returns the process-wide metadata resolver.
//
// It fails when the database is not initialised or when TMDB cannot be
// configured (no TMDB_API_KEY). Callers must degrade rather than 500: the search
// endpoint answers catalogue-only results without it.
func metadataResolver() (*metadata.Resolver, error) {
	resolverMu.Lock()
	defer resolverMu.Unlock()

	if resolverCached != nil {
		return resolverCached, nil
	}

	db, err := database.GetDB()
	if err != nil {
		return nil, fmt.Errorf("database unavailable: %w", err)
	}

	// TMDB is the primary provider; without it there is nothing to ingest.
	tmdbClient, err := tmdb.New()
	if err != nil {
		return nil, err
	}

	// TVDB is supplementary: a missing key degrades the enrichment (the tvdb
	// id link, the certification and trailer fallbacks), it does not stop a
	// title being resolved.
	var tvdbClient *tvdb.Client
	if client, err := tvdb.New(); err == nil {
		tvdbClient = client
	}

	resolverCached = metadata.NewResolver(db, tmdbClient, tvdbClient)
	return resolverCached, nil
}

// errMetadataUnavailable is the API-level wrapper around the resolver's own
// sentinels, so handlers can map failures onto status codes without importing
// the metadata package's error values twice.
func metadataStatusFor(err error) (status int, code, message string) {
	switch {
	case err == nil:
		return 200, "", ""
	case errors.Is(err, metadata.ErrMediaNotFound):
		return 404, "not_found", "media not found"
	case errors.Is(err, metadata.ErrProviderUnavailable):
		return 502, "provider_unavailable", "the metadata provider could not be reached"
	default:
		return 500, "internal", "metadata lookup failed"
	}
}
