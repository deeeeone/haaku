package metadata

// TMDB metadata sub-resources (issue #68): videos, studios, releases,
// certifications, and season/episode videos plus credits.
//
// The contract shape is built once, at ingest/hydration time, and stored in the
// row's metadata JSONB blob under the keys below. Reads then serve the stored
// shape without touching TMDB. Titles ingested before this change carry no
// stored data; Hydrate* fetches it once on the first read and stores it, so the
// second read makes no provider call. A season/episode's `credits` key is only a
// marker that the one-time fetch happened — the credits themselves live in the
// credits table, which is why an honestly-empty credit set is not re-fetched.

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"

	"gorm.io/gorm"
)

// Metadata keys written by the ingest and the lazy hydration.
const (
	KeyVideos         = "videos"
	KeyStudios        = "studios"
	KeyReleases       = "releases"
	KeyCertifications = "certifications"
	KeyCredits        = "credits"
)

// HasKey reports whether a metadata blob carries the given sub-resource key.
// An empty stored array still counts: that is how "TMDB answered with none" is
// told apart from "never fetched".
func HasKey(meta database.JSONMap, key string) bool {
	if meta == nil {
		return false
	}
	_, ok := meta[key]
	return ok
}

// HasCredits reports whether the one-time credit fetch ran for a season or
// episode.
func HasCredits(meta database.JSONMap) bool {
	if meta == nil {
		return false
	}
	value, ok := meta[KeyCredits].(bool)
	return ok && value
}

// StoredObjects renders one of the metadata blob's stored arrays as a JSON
// array. A missing key, a nil value or a malformed value yields a non-nil empty
// slice, so callers always serialize [] rather than null.
//
// The JSON round-trip normalizes the value across the two shapes it can have:
// a freshly built []map[string]any in memory, and the []any of map[string]any
// that a jsonb read produces.
func StoredObjects(meta database.JSONMap, key string) []map[string]any {
	out := make([]map[string]any, 0)
	if meta == nil {
		return out
	}
	raw, ok := meta[key]
	if !ok || raw == nil {
		return out
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return out
	}
	if err := json.Unmarshal(encoded, &out); err != nil {
		return make([]map[string]any, 0)
	}
	if out == nil {
		return make([]map[string]any, 0)
	}
	return out
}

// --- TMDB -> contract shape ---

// VideosFromTMDB converts TMDB videos to the stored contract shape.
//
// Only sites whose watch URL this instance can build are kept: the contract's
// `url` is a full watch URL, and inventing one for an unknown site would be a
// fabricated field. TMDB's video type is already the Trakt vocabulary in
// title case, so it only needs lower-casing.
func VideosFromTMDB(videos []tmdb.Video) []contractshape.Video {
	out := make([]contractshape.Video, 0, len(videos))
	for _, v := range videos {
		url := videoWatchURL(v.Site, v.Key)
		if url == "" {
			continue
		}
		out = append(out, contractshape.Video{
			Title:       v.Name,
			URL:         url,
			Site:        v.Site,
			Type:        strings.ToLower(strings.TrimSpace(v.Type)),
			Size:        v.Size,
			Official:    v.Official,
			PublishedAt: v.PublishedAt,
			Country:     v.ISO3166,
			Language:    v.ISO639,
		})
	}
	return out
}

// videoWatchURL builds the full watch URL for a TMDB video, or "" when the site
// is one this instance cannot address.
func videoWatchURL(site, key string) string {
	if key == "" {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(site)) {
	case "youtube":
		return "https://youtube.com/watch?v=" + key
	case "vimeo":
		return "https://vimeo.com/" + key
	default:
		return ""
	}
}

// StudiosFromTMDB converts TMDB production companies to the stored contract
// shape. TMDB has no Trakt id for a company, so the contract's numeric id is
// derived from a namespaced hash of the company name (see
// contractshape.StableNum) and the slug from the name.
func StudiosFromTMDB(companies []tmdb.ProductionCompany) []contractshape.Studio {
	out := make([]contractshape.Studio, 0, len(companies))
	for _, c := range companies {
		slug := contractshape.Slugify(c.Name)
		if slug == "" {
			slug = fmt.Sprintf("studio-%d", c.ID)
		}
		out = append(out, contractshape.Studio{
			Name:    c.Name,
			Country: c.OriginCountry,
			Slug:    slug,
			TraktID: contractshape.StableNum("studio", c.Name),
			TMDBID:  c.ID,
		})
	}
	return out
}

// ReleasesFromTMDB flattens TMDB's per-country release list into the contract's
// one-row-per-release shape. The country is lower-cased (Trakt's two-letter
// form); the date is TMDB's own date-time string.
func ReleasesFromTMDB(releaseDates *tmdb.ReleaseDates) []contractshape.Release {
	if releaseDates == nil {
		return nil
	}
	out := make([]contractshape.Release, 0)
	for _, country := range releaseDates.Results {
		code := strings.ToLower(strings.TrimSpace(country.ISO3166))
		for _, release := range country.ReleaseDates {
			out = append(out, contractshape.Release{
				Country:       code,
				Certification: release.Certification,
				ReleaseDate:   release.ReleaseDate,
				ReleaseType:   releaseType(release.Type),
			})
		}
	}
	return out
}

// releaseType maps TMDB's numeric release type onto the contract's string.
//
// TMDB numbers its types from 1 (premiere); Trakt's vocabulary is offset by one
// (unknown, premiere, limited, theatrical, digital, physical, tv). The strings
// are what the contract carries.
func releaseType(tmdbType int) string {
	switch tmdbType {
	case 1:
		return "premiere"
	case 2:
		return "limited"
	case 3:
		return "theatrical"
	case 4:
		return "digital"
	case 5:
		return "physical"
	case 6:
		return "tv"
	default:
		return "unknown"
	}
}

// CertificationsFromTMDB converts TMDB content ratings into the contract's
// per-country certification list. Empty ratings are dropped — they carry no
// certification to serve.
func CertificationsFromTMDB(contentRatings *tmdb.ContentRatings) []contractshape.Certification {
	if contentRatings == nil {
		return nil
	}
	out := make([]contractshape.Certification, 0, len(contentRatings.Results))
	for _, rating := range contentRatings.Results {
		code := strings.ToLower(strings.TrimSpace(rating.ISO3166))
		if code == "" || rating.Rating == "" {
			continue
		}
		out = append(out, contractshape.Certification{
			Certification: rating.Rating,
			Country:       code,
		})
	}
	return out
}

// --- hydration ---

// HydrateMovieExtras fetches a movie's TMDB record and stores its videos,
// studios and releases in the row's metadata. It returns the updated blob.
//
// The provider's appended sub-resources are pointers: a nil pointer means the
// payload did not carry that object, so its stored key is left alone (the
// change-feed path may hand back a thinner record than GetMovie does). An
// EMPTY non-nil object is stored as an empty list, which is what stops the read
// path re-fetching a title TMDB says has no videos.
func HydrateMovieExtras(db *gorm.DB, client *tmdb.Client, movie *database.Movie) (database.JSONMap, error) {
	if movie == nil || client == nil || movie.TmdbID <= 0 {
		if movie == nil {
			return nil, nil
		}
		return movie.Metadata, nil
	}

	record, err := client.GetMovie(movie.TmdbID)
	if err != nil {
		return movie.Metadata, fmt.Errorf("fetch movie %d metadata: %w", movie.TmdbID, err)
	}

	patch := database.JSONMap{}
	if record.Videos != nil {
		patch[KeyVideos] = contractshape.Videos(VideosFromTMDB(record.Videos.Results))
	}
	if record.ProductionCompanies != nil {
		patch[KeyStudios] = contractshape.Studios(StudiosFromTMDB(record.ProductionCompanies))
	}
	if record.ReleaseDates != nil {
		patch[KeyReleases] = contractshape.Releases(ReleasesFromTMDB(record.ReleaseDates))
	}

	merged := mergeJSONMaps(movie.Metadata, patch)
	if len(patch) > 0 {
		if err := storeMetadata(db, &database.Movie{}, movie.ID, merged); err != nil {
			return movie.Metadata, err
		}
	}
	movie.Metadata = merged
	return merged, nil
}

// HydrateShowExtras is HydrateMovieExtras for a show: videos, studios and the
// per-country content ratings.
func HydrateShowExtras(db *gorm.DB, client *tmdb.Client, show *database.Show) (database.JSONMap, error) {
	if show == nil || client == nil || show.TmdbID <= 0 {
		if show == nil {
			return nil, nil
		}
		return show.Metadata, nil
	}

	record, err := client.GetShow(show.TmdbID)
	if err != nil {
		return show.Metadata, fmt.Errorf("fetch show %d metadata: %w", show.TmdbID, err)
	}

	patch := database.JSONMap{}
	if record.Videos != nil {
		patch[KeyVideos] = contractshape.Videos(VideosFromTMDB(record.Videos.Results))
	}
	if record.ProductionCompanies != nil {
		patch[KeyStudios] = contractshape.Studios(StudiosFromTMDB(record.ProductionCompanies))
	}
	if record.ContentRatings != nil {
		patch[KeyCertifications] = contractshape.Certifications(CertificationsFromTMDB(record.ContentRatings))
	}

	merged := mergeJSONMaps(show.Metadata, patch)
	if len(patch) > 0 {
		if err := storeMetadata(db, &database.Show{}, show.ID, merged); err != nil {
			return show.Metadata, err
		}
	}
	show.Metadata = merged
	return merged, nil
}

// HydrateSeasonExtras fetches a season with its videos and credits appended,
// stores the videos in the season's metadata and the credit set in the credits
// table under media_type "season". It returns the updated metadata blob.
func HydrateSeasonExtras(db *gorm.DB, client *tmdb.Client, show *database.Show, season *database.Season) (database.JSONMap, error) {
	if season == nil {
		return nil, nil
	}
	if client == nil || show == nil || show.TmdbID <= 0 {
		return season.Metadata, nil
	}

	record, err := client.GetSeasonExtras(show.TmdbID, season.SeasonNumber)
	if err != nil {
		return season.Metadata, fmt.Errorf("fetch show %d season %d metadata: %w",
			show.TmdbID, season.SeasonNumber, err)
	}

	patch := database.JSONMap{KeyCredits: true}
	if record.Videos != nil {
		patch[KeyVideos] = contractshape.Videos(VideosFromTMDB(record.Videos.Results))
	}
	merged := mergeJSONMaps(season.Metadata, patch)
	if err := storeMetadata(db, &database.Season{}, season.ID, merged); err != nil {
		return season.Metadata, err
	}
	season.Metadata = merged

	if record.Credits != nil {
		if err := IngestCredits(db, "season", season.ID, record.Credits); err != nil {
			return merged, err
		}
	}
	return merged, nil
}

// HydrateEpisodeExtras is HydrateSeasonExtras for one episode. TMDB has no
// all-episode videos endpoint, so this is the one request an episode's videos
// cost; the appended credits are its guest stars and crew.
func HydrateEpisodeExtras(db *gorm.DB, client *tmdb.Client, show *database.Show, season *database.Season, episode *database.Episode) (database.JSONMap, error) {
	if episode == nil {
		return nil, nil
	}
	if client == nil || show == nil || season == nil || show.TmdbID <= 0 {
		return episode.Metadata, nil
	}

	record, err := client.GetEpisodeExtras(show.TmdbID, season.SeasonNumber, episode.EpisodeNumber)
	if err != nil {
		return episode.Metadata, fmt.Errorf("fetch show %d season %d episode %d metadata: %w",
			show.TmdbID, season.SeasonNumber, episode.EpisodeNumber, err)
	}

	patch := database.JSONMap{KeyCredits: true}
	if record.Videos != nil {
		patch[KeyVideos] = contractshape.Videos(VideosFromTMDB(record.Videos.Results))
	}
	merged := mergeJSONMaps(episode.Metadata, patch)
	if err := storeMetadata(db, &database.Episode{}, episode.ID, merged); err != nil {
		return episode.Metadata, err
	}
	episode.Metadata = merged

	credits := episodeCredits(record)
	if credits != nil {
		if err := IngestCredits(db, "episode", episode.ID, credits); err != nil {
			return merged, err
		}
	}
	return merged, nil
}

// episodeCredits picks the cast/crew out of an appended episode payload. TMDB
// puts them under `credits` when the append worked, and the season payload puts
// guest_stars/crew at the top level; both are accepted so one shape is stored.
func episodeCredits(record *tmdb.EpisodeDetail) *tmdb.CreditsResponse {
	if record == nil {
		return nil
	}
	credits := &tmdb.CreditsResponse{}
	if record.Credits != nil {
		credits = record.Credits
	}
	if len(credits.Cast) == 0 {
		credits.Cast = record.GuestStars
	}
	if len(credits.Cast) == 0 {
		credits.Cast = credits.GuestStars
	}
	if len(credits.Crew) == 0 {
		credits.Crew = record.Crew
	}
	if len(credits.Cast) == 0 && len(credits.Crew) == 0 {
		return nil
	}
	return credits
}

// storeMetadata writes one row's metadata column. It updates ONLY that column,
// so a hydration cannot clobber the rest of a row that was last written by an
// ingest in another request.
func storeMetadata(db *gorm.DB, model any, id int64, meta database.JSONMap) error {
	if db == nil || id == 0 {
		return nil
	}
	if err := db.Model(model).Where("id = ?", id).Update("metadata", meta).Error; err != nil {
		return fmt.Errorf("store metadata for row %d: %w", id, err)
	}
	return nil
}
