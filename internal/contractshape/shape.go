// Package contractshape builds response payloads that match the vendored
// Trakt contract (docs/parity/upstream/trakt-openapi.json) for parity routes
// that serve our own data at Trakt's path.
//
// These are response-shaping helpers only: every value comes from a stored row
// or a count over stored rows. Nothing is invented. Fields the store does not
// hold are emitted as null, which is what the contract's optional properties
// allow.
package contractshape

import (
	"strconv"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"gorm.io/gorm"
)

// Date renders a *database.Date as the contract's date string, or nil.
func Date(d *database.Date) any {
	if d == nil || d.IsZero() {
		return nil
	}
	return d.Format("2006-01-02")
}

// Timestamp renders a time.Time in the API-wide millisecond RFC3339 layout.
func Timestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// TimestampPtr renders a nullable timestamp.
func TimestampPtr(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return Timestamp(*t)
}

// NullableString maps the empty string to null (contract fields typed
// [string, null]).
func NullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// NullableInt maps zero to null for contract fields typed [integer, null].
func NullableInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

// Year derives the release year from a date, or nil.
func Year(d *database.Date) any {
	if d == nil || d.IsZero() {
		return nil
	}
	return d.Year()
}

// MovieIDs builds the contract ids object for a movie. `trakt` is our stable
// numeric row id; the external ids come straight off the row.
func MovieIDs(m *database.Movie) map[string]any {
	ids := map[string]any{"trakt": m.ID}
	if m.Slug != "" {
		ids["slug"] = m.Slug
	} else {
		ids["slug"] = "movie-" + strconv.FormatInt(m.ID, 10)
	}
	if m.ImdbID != "" {
		ids["imdb"] = m.ImdbID
	}
	if m.TmdbID != 0 {
		ids["tmdb"] = m.TmdbID
	}
	if m.TvdbID != 0 {
		ids["tvdb"] = m.TvdbID
	}
	return ids
}

// ShowIDs builds the contract ids object for a show.
func ShowIDs(s *database.Show) map[string]any {
	ids := map[string]any{"trakt": s.ID}
	if s.Slug != "" {
		ids["slug"] = s.Slug
	} else {
		ids["slug"] = "show-" + strconv.FormatInt(s.ID, 10)
	}
	if s.ImdbID != "" {
		ids["imdb"] = s.ImdbID
	}
	if s.TmdbID != 0 {
		ids["tmdb"] = s.TmdbID
	}
	if s.TvdbID != 0 {
		ids["tvdb"] = s.TvdbID
	}
	return ids
}

// EpisodeIDs builds the contract ids object for an episode.
func EpisodeIDs(e *database.Episode) map[string]any {
	return map[string]any{"trakt": e.ID}
}

// SeasonIDs builds the contract ids object for a season.
func SeasonIDs(s *database.Season) map[string]any {
	return map[string]any{"trakt": s.ID}
}

// ListIDs builds the contract ids object for a list.
func ListIDs(l *database.List) map[string]any {
	slug := l.Slug
	if slug == "" {
		slug = "list-" + strconv.FormatInt(l.ID, 10)
	}
	return map[string]any{"trakt": l.ID, "slug": slug}
}

// Movie renders a movie in the contract's movie object shape. The contract
// requires title and ids; the rest are populated where the row has them.
func Movie(m *database.Movie) map[string]any {
	images := map[string]any{}
	if m.Images != nil {
		images = map[string]any(m.Images)
	}
	genres := []string{}
	if m.Genres != nil {
		genres = m.Genres.Values()
	}
	return map[string]any{
		"title":                  m.Title,
		"year":                   Year(m.Released),
		"ids":                    MovieIDs(m),
		"images":                 images,
		"tagline":                NullableString(m.Tagline),
		"overview":               NullableString(m.Overview),
		"released":               Date(m.Released),
		"runtime":                NullableInt(m.Runtime),
		"status":                 NullableString(m.Status),
		"rating":                 m.RatingAvg,
		"votes":                  m.RatingCount,
		"language":               NullableString(m.Language),
		"genres":                 genres,
		"certification":          NullableString(m.Certification),
		"original_title":         NullableString(m.OriginalTitle),
		"available_translations": []string{},
	}
}

// Show renders a show in the contract's show object shape. The contract
// requires title and ids.
func Show(s *database.Show) map[string]any {
	images := map[string]any{}
	if s.Images != nil {
		images = map[string]any(s.Images)
	}
	genres := []string{}
	if s.Genres != nil {
		genres = s.Genres.Values()
	}
	return map[string]any{
		"title":                  s.Title,
		"year":                   Year(s.FirstAired),
		"ids":                    ShowIDs(s),
		"images":                 images,
		"overview":               NullableString(s.Overview),
		"first_aired":            Date(s.FirstAired),
		"last_aired":             Date(s.LastAired),
		"aired_episodes":         s.EpisodeCount,
		"runtime":                nil,
		"network":                NullableString(s.Network),
		"status":                 NullableString(s.Status),
		"rating":                 s.RatingAvg,
		"votes":                  s.RatingCount,
		"language":               NullableString(s.Language),
		"genres":                 genres,
		"certification":          NullableString(s.Certification),
		"original_title":         NullableString(s.OriginalTitle),
		"available_translations": []string{},
	}
}

// Episode renders an episode in the contract's episode object shape.
// seasonNumber comes from the parent season row.
func Episode(e *database.Episode, seasonNumber int) map[string]any {
	return map[string]any{
		"season":                 seasonNumber,
		"number":                 e.EpisodeNumber,
		"title":                  NullableString(e.Title),
		"first_aired":            Date(e.AiredAt),
		"released":               nil,
		"effective_release_date": nil,
		"number_abs":             e.AbsoluteNumber,
		"rating":                 nil,
		"votes":                  nil,
		"comment_count":          nil,
		"updated_at":             Timestamp(e.UpdatedAt),
		"available_translations": nil,
		"runtime":                NullableInt(e.Runtime),
		"overview":               NullableString(e.Overview),
		"episode_type":           nil,
		"ids":                    EpisodeIDs(e),
		"original_title":         nil,
		"after_credits":          nil,
		"during_credits":         nil,
		"images":                 map[string]any{"screenshot": []string{}},
	}
}

// User renders the compact user object used by comments and lists. The
// contract requires username, private, deleted and ids.
func User(u *database.User) map[string]any {
	if u == nil {
		return map[string]any{
			"username": "", "private": false, "deleted": true,
			"ids": map[string]any{"trakt": 0, "slug": nil, "uuid": ""},
		}
	}
	return map[string]any{
		"username": u.Username,
		"private":  u.IsPrivate,
		"deleted":  false,
		"name":     NullableString(u.Name),
		"ids": map[string]any{
			"trakt": nil, "slug": NullableString(u.Username), "uuid": u.ID,
		},
	}
}

// SettingsUser renders the fuller user object used by /users/settings. The
// contract requires email, username, private, vip, vip_ep, director, ids,
// joined_at, images, vip_og and vip_years.
func SettingsUser(u *database.User) map[string]any {
	avatar := u.AvatarURL
	return map[string]any{
		"email":     u.Email,
		"username":  u.Username,
		"private":   u.IsPrivate,
		"name":      NullableString(u.Name),
		"vip":       false,
		"vip_ep":    false,
		"director":  false,
		"vip_og":    false,
		"vip_years": 0,
		"ids":       map[string]any{"slug": u.Username, "uuid": u.ID},
		"joined_at": Timestamp(u.CreatedAt),
		"location":  NullableString(u.Location),
		"about":     NullableString(u.Bio),
		"images":    map[string]any{"avatar": map[string]any{"full": avatar}},
	}
}

// List renders a list in the contract's list object shape. The contract
// requires name, privacy, share_link, type, the display/sorting flags,
// timestamps, item_count, comment_count, likes, ids and user.
func List(l *database.List, user *database.User) map[string]any {
	images := map[string]any{}
	if l.CoverURL != "" {
		images["poster"] = []string{l.CoverURL}
	}
	return map[string]any{
		"name":            l.Name,
		"description":     NullableString(l.Description),
		"privacy":         l.Privacy,
		"share_link":      "",
		"type":            "personal",
		"display_numbers": l.IsRanked,
		"allow_comments":  l.AllowComments,
		"sort_by":         l.SortBy,
		"sort_how":        l.SortHow,
		"created_at":      Timestamp(l.CreatedAt),
		"updated_at":      Timestamp(l.UpdatedAt),
		"item_count":      0,
		"comment_count":   l.CommentCount,
		"likes":           l.LikeCount,
		"ids":             ListIDs(l),
		"user":            User(user),
		"images":          images,
	}
}

// UserByIDBatch resolves a set of user ids in one query, keyed by id. The
// returned map omits ids with no row; callers render User(nil) for those.
func UserByIDBatch(db *gorm.DB, ids []string) map[string]*database.User {
	out := make(map[string]*database.User, len(ids))
	if len(ids) == 0 {
		return out
	}
	unique := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	var rows []database.User
	if err := db.Where("id IN ?", unique).Find(&rows).Error; err != nil {
		return out
	}
	for i := range rows {
		out[rows[i].ID] = &rows[i]
	}
	return out
}
