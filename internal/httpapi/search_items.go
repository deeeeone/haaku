package httpapi

import (
	"strconv"
	"strings"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/handlers/media"
	"github.com/haaku-tv/haaku/internal/metadata"
)

// searchItem is one card in a search response.
//
// Both sources render into this ONE shape: a catalogue hit (Source "catalog",
// built from the local row) and a live TMDB hit (Source "tmdb") differ only in
// whether the ids include a local one and whether in_catalog is set. The web
// app therefore has a single card component and a single decision — link
// straight there, or resolve first.
type searchItem struct {
	Type   string       `json:"type"`
	Score  float64      `json:"score"`
	Source string       `json:"source"`
	Movie  *searchMedia `json:"movie,omitempty"`
	Show   *searchMedia `json:"show,omitempty"`
}

// searchMedia is the media payload of a search card. It is a subset of the full
// media response (the fields a card renders) plus in_catalog, so a client can
// tell "this is already in your library" apart from "this exists at the
// provider".
type searchMedia struct {
	IDs       media.IDs      `json:"ids"`
	Title     string         `json:"title"`
	Year      int            `json:"year"`
	Overview  string         `json:"overview,omitempty"`
	Images    map[string]any `json:"images"`
	Rating    float64        `json:"rating,omitempty"`
	Votes     int64          `json:"votes,omitempty"`
	Genres    []string       `json:"genres,omitempty"`
	Network   string         `json:"network,omitempty"`
	InCatalog bool           `json:"in_catalog"`
}

// Sources a search card can come from.
const (
	searchSourceCatalog = "catalog"
	searchSourceTMDB    = "tmdb"
)

// catalogSearchItems renders local hits as cards, using the same builders the
// media endpoints use so ids (including the TVDB link) are complete.
func catalogSearchItems(results []database.SearchResult) []searchItem {
	items := make([]searchItem, 0, len(results))
	for _, result := range results {
		switch result.Type {
		case "movie":
			if result.Movie == nil {
				continue
			}
			response := media.BuildMovieResponse(result.Movie, "full")
			items = append(items, searchItem{
				Type:   "movie",
				Source: searchSourceCatalog,
				Movie: &searchMedia{
					IDs:       response.IDs,
					Title:     response.Title,
					Year:      response.Year,
					Overview:  response.Overview,
					Images:    response.Images,
					Genres:    response.Genres,
					Rating:    ratingOf(response.RatingSummary),
					Votes:     votesOf(response.RatingSummary),
					InCatalog: true,
				},
			})
		case "show":
			if result.Show == nil {
				continue
			}
			response := media.BuildShowResponse(result.Show, "full")
			items = append(items, searchItem{
				Type:   "show",
				Source: searchSourceCatalog,
				Show: &searchMedia{
					IDs:       response.IDs,
					Title:     response.Title,
					Year:      response.Year,
					Overview:  response.Overview,
					Images:    response.Images,
					Genres:    response.Genres,
					Network:   response.Network,
					Rating:    ratingOf(response.RatingSummary),
					Votes:     votesOf(response.RatingSummary),
					InCatalog: true,
				},
			})
		default:
			// Episodes, people and lists are still answered only from the
			// catalogue: TVDB's name search covers movies and series.
			continue
		}
	}
	return items
}

// appendProviderItems adds live provider hits that the catalogue does not
// already have, up to the page limit.
//
// A hit is a duplicate when its TMDB id is already on the page, or when the
// same title and year are already there — the catalogue row and the provider
// record are the same work, and showing it twice looks like two entries.
func appendProviderItems(items []searchItem, hits []metadata.ProviderHit, limit int) []searchItem {
	if limit <= 0 {
		limit = 25
	}

	seenTMDB := map[string]bool{}
	seenTitle := map[string]bool{}
	for _, item := range items {
		card := item.Movie
		mediaType := "movie"
		if item.Show != nil {
			card = item.Show
			mediaType = "show"
		}
		if card == nil {
			continue
		}
		if card.IDs.TMDB != 0 {
			seenTMDB[mediaType+":"+itoa(card.IDs.TMDB)] = true
		}
		seenTitle[titleKey(mediaType, card.Title, card.Year)] = true
	}

	for _, hit := range hits {
		if len(items) >= limit {
			break
		}
		if seenTMDB[hit.MediaType+":"+itoa(hit.TMDBID)] {
			continue
		}
		if seenTitle[titleKey(hit.MediaType, hit.Title, hit.Year)] {
			continue
		}
		seenTMDB[hit.MediaType+":"+itoa(hit.TMDBID)] = true
		seenTitle[titleKey(hit.MediaType, hit.Title, hit.Year)] = true

		card := &searchMedia{
			IDs: media.IDs{
				TMDB: hit.TMDBID,
			},
			Title:     hit.Title,
			Year:      hit.Year,
			Overview:  hit.Overview,
			Images:    map[string]any{"poster": hit.PosterURL},
			InCatalog: hit.InCatalog,
		}
		if hit.InCatalog && hit.MediaID > 0 {
			// A title the catalogue already holds gets its local id, so the
			// card links straight to it (no resolve round trip).
			card.IDs.ID = hit.MediaID
		}

		item := searchItem{Type: hit.MediaType, Source: searchSourceTMDB}
		if hit.MediaType == "movie" {
			item.Movie = card
		} else {
			item.Show = card
		}
		items = append(items, item)
	}

	return items
}

func titleKey(mediaType, title string, year int) string {
	return strings.ToLower(strings.TrimSpace(mediaType)) + ":" +
		strings.ToLower(strings.TrimSpace(title)) + ":" + itoa(year)
}

func ratingOf(summary *media.RatingSummary) float64 {
	if summary == nil {
		return 0
	}
	return summary.Rating
}

func votesOf(summary *media.RatingSummary) int64 {
	if summary == nil {
		return 0
	}
	return summary.Votes
}

// itoa/itoa64 are the pagination helpers; the search headers are strings.
func itoa(n int) string { return strconv.Itoa(n) }

func itoa64(n int64) string { return strconv.FormatInt(n, 10) }

// pageCount is the total page count for a header, never below 1.
func pageCount(total int64, limit int) int {
	if limit <= 0 {
		limit = 25
	}
	pages := int((total + int64(limit) - 1) / int64(limit))
	if pages < 1 {
		pages = 1
	}
	return pages
}
