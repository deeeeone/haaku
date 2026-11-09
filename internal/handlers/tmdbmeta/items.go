package tmdbmeta

import (
	"strings"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Videos
// ---------------------------------------------------------------------------

func handleMovieVideos(c *fiber.Ctx) error {
	db, movie, err := resolveMovie(c)
	if err != nil {
		return err
	}
	return serveVideos(c, db, "movie", movie.ID, func() (*tmdb.Videos, error) {
		client, cerr := getTMDBClient()
		if cerr != nil {
			return nil, cerr
		}
		return client.GetMovieVideos(movie.TmdbID)
	})
}

func handleShowVideos(c *fiber.Ctx) error {
	db, show, err := resolveShow(c)
	if err != nil {
		return err
	}
	return serveVideos(c, db, "show", show.ID, func() (*tmdb.Videos, error) {
		client, cerr := getTMDBClient()
		if cerr != nil {
			return nil, cerr
		}
		return client.GetShowVideos(show.TmdbID)
	})
}

func handleSeasonVideos(c *fiber.Ctx) error {
	db, show, season, err := resolveShowSeason(c)
	if err != nil {
		return err
	}
	return serveVideos(c, db, "season", season.ID, func() (*tmdb.Videos, error) {
		client, cerr := getTMDBClient()
		if cerr != nil {
			return nil, cerr
		}
		return client.GetSeasonVideos(show.TmdbID, season.SeasonNumber)
	})
}

func handleEpisodeVideos(c *fiber.Ctx) error {
	db, show, season, episode, err := resolveShowEpisode(c)
	if err != nil {
		return err
	}
	return serveVideos(c, db, "episode", episode.ID, func() (*tmdb.Videos, error) {
		client, cerr := getTMDBClient()
		if cerr != nil {
			return nil, cerr
		}
		return client.GetEpisodeVideos(show.TmdbID, season.SeasonNumber, episode.EpisodeNumber)
	})
}

func serveVideos(c *fiber.Ctx, db *gorm.DB, mediaType string, mediaID int64, fetch func() (*tmdb.Videos, error)) error {
	payload, err := fetchOrStored(c, db, mediaType, mediaID, "videos", func() (database.JSONMap, error) {
		videos, ferr := fetch()
		if ferr != nil {
			return nil, ferr
		}
		return toPayload(map[string]any{"videos": contractVideos(videos)})
	})
	if err != nil {
		return err
	}
	return c.JSON(arrayValue(payload, "videos"))
}

// contractVideos maps TMDB's video set onto the contract's array. Only entries
// we can turn into a real, embeddable URL are kept (YouTube); a video whose URL
// we cannot build is left out rather than emitted with an empty url.
func contractVideos(v *tmdb.Videos) []map[string]any {
	out := make([]map[string]any, 0)
	if v == nil {
		return out
	}
	for _, video := range v.Results {
		if video.Site != "YouTube" || video.Key == "" {
			continue
		}
		out = append(out, map[string]any{
			"title":        video.Name,
			"url":          "https://www.youtube.com/watch?v=" + video.Key,
			"site":         video.Site,
			"type":         video.Type,
			"size":         video.Size,
			"official":     video.Official,
			"published_at": video.PublishedAt,
			"country":      video.ISO3166,
			"language":     video.ISO639,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Releases (movie, per country)
// ---------------------------------------------------------------------------

// releaseTypeName maps TMDB's numeric release type onto the contract's string.
func releaseTypeName(t int) string {
	switch t {
	case 1:
		return "premiere"
	case 2, 3:
		return "theatrical"
	case 4:
		return "digital"
	case 5:
		return "physical"
	case 6:
		return "tv"
	default:
		return "theatrical"
	}
}

func handleMovieReleases(c *fiber.Ctx) error {
	db, movie, err := resolveMovie(c)
	if err != nil {
		return err
	}
	country := strings.ToUpper(strings.TrimSpace(c.Params("country")))
	if len(country) != 2 {
		return response.BadRequest(c, "invalid country code: must be a 2-letter ISO code")
	}

	payload, err := fetchOrStored(c, db, "movie", movie.ID, "releases", func() (database.JSONMap, error) {
		client, cerr := getTMDBClient()
		if cerr != nil {
			return nil, cerr
		}
		rd, ferr := client.GetMovieReleaseDates(movie.TmdbID)
		if ferr != nil {
			return nil, ferr
		}
		return toPayload(map[string]any{"releases": contractReleases(rd)})
	})
	if err != nil {
		return err
	}

	filtered := make([]any, 0)
	for _, item := range arrayValue(payload, "releases") {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if code, _ := row["country"].(string); strings.EqualFold(code, country) {
			filtered = append(filtered, row)
		}
	}
	return c.JSON(filtered)
}

// contractReleases flattens TMDB's per-country release dates into the
// contract's array of {country, certification, release_date, release_type,
// note}. TMDB carries no free-form note, so note is null.
func contractReleases(rd *tmdb.ReleaseDates) []map[string]any {
	out := make([]map[string]any, 0)
	if rd == nil {
		return out
	}
	for _, country := range rd.Results {
		for _, release := range country.ReleaseDates {
			out = append(out, map[string]any{
				"country":       country.ISO3166,
				"certification": nullableString(release.Certification),
				"release_date":  release.ReleaseDate,
				"release_type":  releaseTypeName(release.Type),
				"note":          nil,
			})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Translations
// ---------------------------------------------------------------------------

func handleMovieTranslations(c *fiber.Ctx) error {
	db, movie, err := resolveMovie(c)
	if err != nil {
		return err
	}
	return serveTranslations(c, db, "movie", movie.ID, func() (*tmdb.Translations, error) {
		client, cerr := getTMDBClient()
		if cerr != nil {
			return nil, cerr
		}
		return client.GetMovieTranslations(movie.TmdbID)
	}, true)
}

func handleShowTranslations(c *fiber.Ctx) error {
	db, show, err := resolveShow(c)
	if err != nil {
		return err
	}
	return serveTranslations(c, db, "show", show.ID, func() (*tmdb.Translations, error) {
		client, cerr := getTMDBClient()
		if cerr != nil {
			return nil, cerr
		}
		return client.GetShowTranslations(show.TmdbID)
	}, true)
}

func handleEpisodeTranslations(c *fiber.Ctx) error {
	db, show, season, episode, err := resolveShowEpisode(c)
	if err != nil {
		return err
	}
	return serveTranslations(c, db, "episode", episode.ID, func() (*tmdb.Translations, error) {
		client, cerr := getTMDBClient()
		if cerr != nil {
			return nil, cerr
		}
		return client.GetEpisodeTranslations(show.TmdbID, season.SeasonNumber, episode.EpisodeNumber)
	}, false)
}

func serveTranslations(
	c *fiber.Ctx,
	db *gorm.DB,
	mediaType string,
	mediaID int64,
	fetch func() (*tmdb.Translations, error),
	withTagline bool,
) error {
	payload, err := fetchOrStored(c, db, mediaType, mediaID, "translations", func() (database.JSONMap, error) {
		tr, ferr := fetch()
		if ferr != nil {
			return nil, ferr
		}
		return toPayload(map[string]any{"translations": contractTranslations(tr, withTagline)})
	})
	if err != nil {
		return err
	}

	lang := strings.ToLower(strings.TrimSpace(c.Query("language")))
	all := arrayValue(payload, "translations")
	if lang == "" {
		return c.JSON(all)
	}
	filtered := make([]any, 0)
	for _, item := range all {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if code, _ := row["language"].(string); strings.EqualFold(code, lang) {
			filtered = append(filtered, row)
		}
	}
	return c.JSON(filtered)
}

// contractTranslations maps TMDB's translation set onto the contract's array.
// Movies fill title/tagline; episodes fill title from `name`.
func contractTranslations(tr *tmdb.Translations, withTagline bool) []map[string]any {
	out := make([]map[string]any, 0)
	if tr == nil {
		return out
	}
	for _, t := range tr.Translations {
		title := t.Data.Title
		if title == "" {
			title = t.Data.Name
		}
		tagline := any(nil)
		if withTagline {
			tagline = nullableString(t.Data.Tagline)
		}
		out = append(out, map[string]any{
			"title":    nullableString(title),
			"overview": nullableString(t.Data.Overview),
			"tagline":  tagline,
			"language": t.ISO639,
			"country":  nullableString(t.ISO3166),
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Season info
// ---------------------------------------------------------------------------

func handleSeasonInfo(c *fiber.Ctx) error {
	db, show, season, err := resolveShowSeason(c)
	if err != nil {
		return err
	}

	payload, err := fetchOrStored(c, db, "season", season.ID, "season_info", func() (database.JSONMap, error) {
		client, cerr := getTMDBClient()
		if cerr != nil {
			return nil, cerr
		}
		detail, ferr := client.GetSeason(show.TmdbID, season.SeasonNumber)
		if ferr != nil {
			return nil, ferr
		}
		return toPayload(contractSeasonInfo(season, detail))
	})
	if err != nil {
		return err
	}
	return c.JSON(payload)
}

// contractSeasonInfo builds the contract's season object from our season row
// and TMDB's season detail. rating is null because we store no season rating;
// votes and title/overview come from the provider payload.
func contractSeasonInfo(season *database.Season, detail *tmdb.Season) map[string]any {
	images := map[string]any{"poster": []string{}, "thumb": []string{}}
	if season.Images != nil {
		if p, ok := season.Images["poster"].(string); ok && p != "" {
			images["poster"] = []string{p}
		}
	}
	title := any(nil)
	overview := any(nil)
	var votes any
	var tmdbID any
	firstAired := any(nil)
	if detail != nil {
		title = nullableString(detail.Name)
		overview = nullableString(detail.Overview)
		if detail.VoteCount > 0 {
			votes = detail.VoteCount
		}
		if detail.ID != 0 {
			tmdbID = detail.ID
		}
		if detail.AirDate != "" {
			firstAired = detail.AirDate
		}
	}
	if firstAired == nil {
		firstAired = dateOrNil(season.AiredAt)
	}
	return map[string]any{
		"number":         season.SeasonNumber,
		"ids":            map[string]any{"trakt": season.ID, "tvdb": nil, "tmdb": tmdbID},
		"aired_episodes": season.EpisodeCount,
		"rating":         nil,
		"votes":          votes,
		"episode_count":  season.EpisodeCount,
		"title":          title,
		"overview":       overview,
		"first_aired":    firstAired,
		"updated_at":     season.UpdatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		"total_runtime":  nil,
		"network":        nullableString(showNetwork(detail)),
		"images":         images,
	}
}

// showNetwork is always nil here: TMDB's season detail carries no network, and
// the network lives on the show row. Left explicit so the field is a deliberate
// null rather than an omission.
func showNetwork(_ *tmdb.Season) string { return "" }

// ---------------------------------------------------------------------------
// Studios
// ---------------------------------------------------------------------------

func handleMovieStudios(c *fiber.Ctx) error {
	db, movie, err := resolveMovie(c)
	if err != nil {
		return err
	}
	payload, err := fetchOrStored(c, db, "movie", movie.ID, "studios", func() (database.JSONMap, error) {
		client, cerr := getTMDBClient()
		if cerr != nil {
			return nil, cerr
		}
		detail, ferr := client.GetMovie(movie.TmdbID)
		if ferr != nil {
			return nil, ferr
		}
		return toPayload(map[string]any{"studios": contractStudiosFromCompanies(detail.ProductionCompanies)})
	})
	if err != nil {
		return err
	}
	return c.JSON(arrayValue(payload, "studios"))
}

func handleShowStudios(c *fiber.Ctx) error {
	db, show, err := resolveShow(c)
	if err != nil {
		return err
	}
	payload, err := fetchOrStored(c, db, "show", show.ID, "studios", func() (database.JSONMap, error) {
		client, cerr := getTMDBClient()
		if cerr != nil {
			return nil, cerr
		}
		detail, ferr := client.GetShow(show.TmdbID)
		if ferr != nil {
			return nil, ferr
		}
		return toPayload(map[string]any{"studios": contractStudiosFromNetworks(detail.Networks)})
	})
	if err != nil {
		return err
	}
	return c.JSON(arrayValue(payload, "studios"))
}

// contractStudiosFromCompanies maps TMDB production companies to the contract's
// studio shape. We hold no Trakt company ids, so ids.trakt is null and
// ids.tmdb carries the TMDB company id; the row is graded divergent for exactly
// that reason.
func contractStudiosFromCompanies(companies []tmdb.Company) []map[string]any {
	out := make([]map[string]any, 0, len(companies))
	for _, co := range companies {
		out = append(out, map[string]any{
			"name":    co.Name,
			"country": nullableString(co.OriginCountry),
			"ids":     map[string]any{"slug": slugify(co.Name), "trakt": nil, "tmdb": nullableInt(co.ID)},
		})
	}
	return out
}

func contractStudiosFromNetworks(networks []tmdb.Network) []map[string]any {
	out := make([]map[string]any, 0, len(networks))
	for _, n := range networks {
		out = append(out, map[string]any{
			"name":    n.Name,
			"country": nullableString(n.OriginCountry),
			"ids":     map[string]any{"slug": slugify(n.Name), "trakt": nil, "tmdb": nullableInt(n.ID)},
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Certifications
// ---------------------------------------------------------------------------

// handleShowCertifications serves the show's stored certification. The vendored
// contract types this response as a bare string, so the stored single value is
// exactly the shape asked for.
func handleShowCertifications(c *fiber.Ctx) error {
	_, show, err := resolveShow(c)
	if err != nil {
		return err
	}
	return c.JSON(show.Certification)
}

// ---------------------------------------------------------------------------
// small helpers
// ---------------------------------------------------------------------------

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func dateOrNil(d *database.Date) any {
	if d == nil || d.IsZero() {
		return nil
	}
	return d.Format("2006-01-02")
}

// slugify builds a stable slug from a studio name (lower-case, non-alphanumeric
// runs collapsed to a hyphen).
func slugify(name string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
