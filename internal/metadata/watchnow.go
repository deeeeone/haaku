package metadata

// Watch-now provider catalogue (issue #69).
//
// TMDB's /watch/providers/{movie,tv} payload is a provider CATALOGUE, not a
// per-title offer: each provider names every country it is available in and its
// display priority there. RefreshWatchNowSources replaces the stored catalogue
// in one transaction, so a read either sees the old complete set or the new one.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Watch-now catalogue media types as TMDB spells them in the path.
const (
	watchNowMovie = "movie"
	watchNowTV    = "tv"
)

// RefreshWatchNowSources fetches both provider catalogues from TMDB and
// replaces the stored watchnow_sources rows.
func RefreshWatchNowSources(ctx context.Context, client *tmdb.Client, db *gorm.DB) error {
	if client == nil || db == nil {
		return errors.New("metadata: a TMDB client and a database are required")
	}

	var rows []database.WatchNowSource
	for _, mediaType := range []string{watchNowMovie, watchNowTV} {
		if err := ctx.Err(); err != nil {
			return err
		}
		catalogue, err := client.GetWatchProviderCatalogue(mediaType)
		if err != nil {
			return fmt.Errorf("fetch watch provider catalogue (%s): %w", mediaType, err)
		}
		rows = append(rows, watchNowSourceRows(catalogue, mediaType)...)
	}

	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&database.WatchNowSource{}).Error; err != nil {
			return fmt.Errorf("clear watch now sources: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).
			CreateInBatches(rows, 200).Error; err != nil {
			return fmt.Errorf("write watch now sources: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	slog.Info("[watchnow] provider catalogue refreshed", "rows", len(rows))
	return nil
}

// watchNowSourceRows flattens one catalogue into rows, one per provider per
// country. The source slug is derived from the provider name (TMDB has no
// Trakt-style source key) and the priority is TMDB's own display priority for
// that country.
func watchNowSourceRows(catalogue *tmdb.WatchProvidersCatalogue, mediaType string) []database.WatchNowSource {
	if catalogue == nil {
		return nil
	}
	out := make([]database.WatchNowSource, 0, len(catalogue.Results))
	seen := make(map[string]bool, len(catalogue.Results))
	for _, provider := range catalogue.Results {
		if provider.ProviderID <= 0 || provider.ProviderName == "" {
			continue
		}
		source := contractshape.Slugify(provider.ProviderName)
		if source == "" {
			source = fmt.Sprintf("provider-%d", provider.ProviderID)
		}
		for country, priority := range provider.DisplayPriorities {
			country = normalizeCountry(country)
			if len(country) != 2 {
				continue
			}
			key := fmt.Sprintf("%d|%s", provider.ProviderID, country)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, database.WatchNowSource{
				MediaType:  mediaType,
				ProviderID: provider.ProviderID,
				Source:     source,
				Name:       provider.ProviderName,
				Country:    country,
				Priority:   priority,
				LogoPath:   provider.LogoPath,
			})
		}
	}
	return out
}

// normalizeCountry lower-cases and trims a country code.
func normalizeCountry(country string) string {
	out := make([]byte, 0, len(country))
	for i := 0; i < len(country); i++ {
		c := country[i]
		if c == ' ' || c == '\t' {
			continue
		}
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out = append(out, c)
	}
	return string(out)
}
