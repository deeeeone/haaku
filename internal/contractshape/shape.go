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
	"hash/fnv"
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

// ParentID renders a comment's parent id in the contract's comment shape.
//
// The contract types parent_id as a *required* integer and gives it no null
// alternative, so a top-level comment cannot be expressed as null — that made
// every top-level comment fail the response schema. Trakt's own payloads use 0
// for "no parent", which is the only value the declared type admits as a
// sentinel; it is a marker, never a comment id (ids are positive), and every
// reader of the field must treat 0 as top-level.
func ParentID(parent *int64) int64 {
	if parent == nil {
		return 0
	}
	return *parent
}

// Year derives the release year from a date, or nil.
func Year(d *database.Date) any {
	if d == nil || d.IsZero() {
		return nil
	}
	return d.Year()
}

// idsMap is the contract's media ids object: the numeric trakt id, the required
// slug, and each external id present in the contract's schema. A value we do
// not hold is emitted as JSON null — never an empty string, never a fabricated
// number — which is exactly what the contract's `[type, null]` ids allow.
func idsMap(trakt int64, slug string, imdb string, tmdb, tvdb int) map[string]any {
	return map[string]any{
		"trakt": trakt,
		"slug":  slug,
		"imdb":  NullableString(imdb),
		"tmdb":  NullableInt(tmdb),
		"tvdb":  NullableInt(tvdb),
	}
}

// localSlug returns the row's slug, or the stable local slug derived from the
// numeric id when ingest stored none. The contract requires `slug` to be a
// non-null string on movies and shows, so this is the one required id we cannot
// emit as null; the value is deterministic and derived from our own row, not
// from an external service.
func localSlug(slug, kind string, id int64) string {
	if slug != "" {
		return slug
	}
	return kind + "-" + strconv.FormatInt(id, 10)
}

// MovieIDs builds the contract ids object for a movie. `trakt` is our stable
// numeric row id; the external ids come straight off the row.
func MovieIDs(m *database.Movie) map[string]any {
	return idsMap(m.ID, localSlug(m.Slug, "movie", m.ID), m.ImdbID, m.TmdbID, m.TvdbID)
}

// ShowIDs builds the contract ids object for a show.
func ShowIDs(s *database.Show) map[string]any {
	return idsMap(s.ID, localSlug(s.Slug, "show", s.ID), s.ImdbID, s.TmdbID, s.TvdbID)
}

// EpisodeIDs builds the contract ids object for an episode. The episodes table
// stores no external ids, so those keys are null; the contract requires only
// `trakt`.
func EpisodeIDs(e *database.Episode) map[string]any {
	return idsMap(e.ID, "", "", 0, 0)
}

// PersonIDs builds the contract ids object for a person. The contract requires
// slug and trakt; the row has no slug column, so the same stable local fallback
// the movie/show helpers use is derived from the row id.
func PersonIDs(p *database.Person) map[string]any {
	ids := map[string]any{
		"trakt": p.ID,
		"slug":  "person-" + strconv.FormatInt(p.ID, 10),
	}
	if p.ImdbID != "" {
		ids["imdb"] = p.ImdbID
	}
	if p.TmdbID != 0 {
		ids["tmdb"] = p.TmdbID
	}
	return ids
}

// SeasonIDs builds the contract ids object for a season. The seasons table
// stores no external ids, so those keys are null; the contract requires only
// `trakt`.
func SeasonIDs(s *database.Season) map[string]any {
	return map[string]any{
		"trakt": s.ID,
		"tvdb":  nil,
		"tmdb":  nil,
		"imdb":  nil,
	}
}

// Season renders a season in the contract's season object shape. The contract
// requires number and ids; aired_episodes is emitted from the row.
//
// first_aired is optional-with-no-null in the contract, so it is omitted when
// the row has no air date rather than emitted as null. images is always the
// contract's {poster, thumb} arrays, populated from the stored image blob when
// it carries those keys.
func Season(s *database.Season) map[string]any {
	images := map[string]any{"poster": []string{}, "thumb": []string{}}
	if s.Images != nil {
		for _, key := range []string{"poster", "thumb"} {
			if value, ok := s.Images[key]; ok {
				images[key] = value
			}
		}
	}
	out := map[string]any{
		"number":         s.SeasonNumber,
		"ids":            SeasonIDs(s),
		"aired_episodes": s.EpisodeCount,
		"episode_count":  s.EpisodeCount,
		"title":          nil,
		"overview":       nil,
		"rating":         nil,
		"votes":          nil,
		"total_runtime":  nil,
		"network":        nil,
		"updated_at":     Timestamp(s.UpdatedAt),
		"images":         images,
	}
	if s.AiredAt != nil && !s.AiredAt.IsZero() {
		out["first_aired"] = s.AiredAt.Format("2006-01-02")
	}
	return out
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

// Person renders a person in the contract's person object shape. The contract
// requires name and ids; fields it types as null are emitted as null rather
// than inventing a value the store does not hold.
func Person(p *database.Person) map[string]any {
	images := map[string]any{"headshot": []string{}, "fanart": []string{}}
	if p.Images != nil {
		if v, ok := p.Images["headshot"]; ok {
			images["headshot"] = v
		}
		if v, ok := p.Images["fanart"]; ok {
			images["fanart"] = v
		}
	}
	return map[string]any{
		"name":                 p.Name,
		"ids":                  PersonIDs(p),
		"social_ids":           nil,
		"biography":            NullableString(p.Biography),
		"summary":              nil,
		"birthday":             Date(p.Birthday),
		"height":               nil,
		"death":                Date(p.Deathday),
		"birthplace":           nil,
		"homepage":             nil,
		"gender":               genderEnum(p.Gender),
		"known_for_department": crewDepartment(p.KnownForDepartment),
		"updated_at":           Timestamp(p.UpdatedAt),
		"images":               images,
	}
}

// genderEnum maps TMDB's numeric gender onto the contract's enum. TMDB numbers
// it 0 unknown, 1 female, 2 male, 3 non-binary.
func genderEnum(gender int) any {
	switch gender {
	case 1:
		return "female"
	case 2:
		return "male"
	case 3:
		return "non_binary"
	default:
		return "unknown"
	}
}

// Profile renders the contract's profileResponseSchema from a user row. It is
// used wherever a list of accounts is embedded (the "watching right now"
// routes). Fields the store does not hold are null; ids uses the same stable
// local numeric derivation as every other embedded user.
func Profile(u *database.User) map[string]any {
	out := map[string]any{
		"username":  u.Username,
		"private":   u.IsPrivate,
		"deleted":   false,
		"name":      NullableString(u.Name),
		"vip":       false,
		"vip_ep":    false,
		"director":  false,
		"ids":       map[string]any{"slug": NullableString(u.Username), "trakt": LocalUserNum(u.ID)},
		"joined_at": Timestamp(u.CreatedAt),
		"location":  NullableString(u.Location),
		"about":     NullableString(u.Bio),
		"gender":    nil,
		"age":       nil,
	}
	if u.AvatarURL != "" {
		out["images"] = map[string]any{"avatar": map[string]any{"full": u.AvatarURL}}
	}
	return out
}

// User renders the compact user object used by comments and lists. The
// contract requires username, private, deleted and ids.
func User(u *database.User) map[string]any {
	if u == nil {
		return map[string]any{
			"username": "", "private": false, "deleted": true,
			"ids": map[string]any{"trakt": int64(0), "slug": nil},
		}
	}
	return map[string]any{
		"username": u.Username,
		"private":  u.IsPrivate,
		"deleted":  false,
		"name":     NullableString(u.Name),
		"ids": map[string]any{
			"trakt": LocalUserNum(u.ID),
			"slug":  NullableString(u.Username),
		},
	}
}

// LocalUserNum derives the numeric id the contract's user object requires.
//
// The embedded user object has exactly two id properties, `slug` and `trakt`,
// and requires `trakt` to be an *integer*. A self-hosted account's primary key is
// a string (a Clerk-style "user_..." id, or "local_..."), so there is no number to
// emit — and a null in a required integer field makes a conforming client reject
// the whole feed.
//
// The number is therefore derived from the primary key rather than invented per
// response: it is stable across requests and restarts, and the same account has
// the same number everywhere it is embedded. It is haaku's local numeric id for
// the account, NOT Trakt's, and it is never used as a lookup key — routes address
// a user by slug or by uuid. 31 bits keeps it inside every JSON/JS integer range
// a client might use; with a self-hosted instance's account count, a collision is
// not a practical concern (and the slug remains the unique handle).
func LocalUserNum(id string) int64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	return int64(h.Sum32() & 0x7fffffff)
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
	return ListWithItemCount(l, user, 0)
}

// ListWithItemCount is List with the caller-supplied real item count. Callers
// that have the count (a grouped list_items query) use this rather than emitting
// the contract's required item_count as a fabricated zero.
func ListWithItemCount(l *database.List, user *database.User, itemCount int64) map[string]any {
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
		"item_count":      itemCount,
		"comment_count":   l.CommentCount,
		"likes":           l.LikeCount,
		"ids":             ListIDs(l),
		"user":            User(user),
		"images":          images,
	}
}

// UserByIDBatch resolves a set of user ids in one query, keyed by id. The
// returned map omits ids with no row; callers render User(nil) for those.
//
// The query error is returned rather than swallowed: a transient database failure
// used to come back as an empty map, and every owner rendered as User(nil) —
// which says "deleted": true. A 2xx that marks every list owner deleted is worse
// than a 500.
func UserByIDBatch(db *gorm.DB, ids []string) (map[string]*database.User, error) {
	out := make(map[string]*database.User, len(ids))
	if len(ids) == 0 {
		return out, nil
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
		return out, err
	}
	for i := range rows {
		out[rows[i].ID] = &rows[i]
	}
	return out, nil
}
