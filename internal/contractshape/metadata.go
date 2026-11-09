package contractshape

// TMDB-derived objects (issue #68): videos, studios, releases, per-country
// certifications and the people object. Every value here comes from a stored
// TMDB record or a credit row; nothing is invented. The builders exist so the
// ingest can store the contract shape ONCE and the read paths serve it without
// re-mapping (store once, serve from the DB).

import (
	"hash/fnv"
	"strings"

	"github.com/haaku-tv/haaku/internal/database"
)

// --- videos ---

// Video is one trailer/teaser/clip in the contract's video object shape.
type Video struct {
	Title       string
	URL         string
	Site        string
	Type        string
	Size        int
	Official    bool
	PublishedAt string
	Country     string
	Language    string
}

// Videos renders stored videos in the contract shape. A non-nil empty slice is
// returned so an empty result serializes as [] rather than null.
func Videos(videos []Video) []map[string]any {
	out := make([]map[string]any, 0, len(videos))
	for _, v := range videos {
		out = append(out, map[string]any{
			"title":        v.Title,
			"url":          v.URL,
			"site":         v.Site,
			"type":         v.Type,
			"size":         v.Size,
			"official":     v.Official,
			"published_at": v.PublishedAt,
			"country":      v.Country,
			"language":     v.Language,
		})
	}
	return out
}

// --- studios ---

// Studio is one production company in the contract's studio object shape.
type Studio struct {
	Name    string
	Country string
	Slug    string
	TraktID int64
	TMDBID  int
}

// Studios renders stored studios in the contract shape.
func Studios(studios []Studio) []map[string]any {
	out := make([]map[string]any, 0, len(studios))
	for _, s := range studios {
		out = append(out, map[string]any{
			"name":    s.Name,
			"country": NullableString(s.Country),
			"ids": map[string]any{
				"slug":  s.Slug,
				"trakt": s.TraktID,
				"tmdb":  NullableInt(s.TMDBID),
			},
		})
	}
	return out
}

// StableNum derives a stable positive integer for an object that has no local
// row id of its own.
//
// A TMDB studio lives inside a title's metadata blob rather than in a table,
// but the contract requires ids.trakt to be an integer. Hashing a namespaced
// key gives the same number on every request and restart without inventing a
// second identity; 31 bits keeps it inside every JSON/JS integer range a client
// might use. This is haaku's local numeric id for the studio, NOT Trakt's.
func StableNum(namespace, key string) int64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(namespace))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(key))
	return int64(h.Sum32() & 0x7fffffff)
}

// Slugify reduces a name to a URL-safe slug: "Lionsgate Films" ->
// "lionsgate-films". It is the same rule the ingest uses for media slugs, kept
// here so a studio's contract id object can carry a stable slug without a
// studio table.
func Slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// --- releases ---

// Release is one dated release for one country in the contract's shape.
type Release struct {
	Country       string
	Certification string
	ReleaseDate   string
	ReleaseType   string
	Note          string
}

// Releases renders stored releases in the contract shape.
func Releases(releases []Release) []map[string]any {
	out := make([]map[string]any, 0, len(releases))
	for _, r := range releases {
		out = append(out, map[string]any{
			"country":       r.Country,
			"certification": NullableString(r.Certification),
			"release_date":  r.ReleaseDate,
			"release_type":  r.ReleaseType,
			"note":          NullableString(r.Note),
		})
	}
	return out
}

// --- certifications ---

// Certification is one country's content rating for a show.
type Certification struct {
	Certification string
	Country       string
}

// Certifications renders stored content ratings in the contract shape.
func Certifications(certifications []Certification) []map[string]any {
	out := make([]map[string]any, 0, len(certifications))
	for _, c := range certifications {
		out = append(out, map[string]any{
			"certification": c.Certification,
			"country":       c.Country,
		})
	}
	return out
}

// --- translations ---

// Translation is one language/country variant in the contract's shape. Movies
// and shows carry a tagline; episodes do not, so an empty tagline serializes as
// null and is accepted by both translation schemas.
type Translation struct {
	Title    string
	Overview string
	Tagline  string
	Language string
	Country  string
}

// Translations renders stored translations in the contract's array shape. A
// non-nil empty slice is returned so an empty result serializes as [].
func Translations(translations []Translation) []map[string]any {
	out := make([]map[string]any, 0, len(translations))
	for _, t := range translations {
		out = append(out, map[string]any{
			"title":    NullableString(t.Title),
			"overview": NullableString(t.Overview),
			"tagline":  NullableString(t.Tagline),
			"language": t.Language,
			"country":  NullableString(t.Country),
		})
	}
	return out
}

// --- people ---

// People renders a credit set in the contract's peopleResponseSchema shape:
// cast entries carry character/characters, crew entries are grouped by
// department and carry job/jobs. Credits whose person row is missing are
// skipped rather than rendered as a nameless ghost.
func People(credits []database.Credit, persons map[int64]*database.Person) map[string]any {
	cast := make([]map[string]any, 0)
	crewByDepartment := make(map[string][]map[string]any)

	for _, cr := range credits {
		person := persons[cr.PersonID]
		if person == nil {
			continue
		}
		if cr.CreditType == "cast" {
			characters := make([]string, 0, 1)
			if cr.Character != "" {
				characters = append(characters, cr.Character)
			}
			cast = append(cast, map[string]any{
				"character":     cr.Character,
				"characters":    characters,
				"episode_count": NullableInt(cr.EpisodeCount),
				"person":        Person(person),
			})
			continue
		}

		jobs := make([]string, 0, 1)
		if cr.Job != "" {
			jobs = append(jobs, cr.Job)
		}
		department := crewDepartment(cr.Department)
		crewByDepartment[department] = append(crewByDepartment[department], map[string]any{
			"job":           cr.Job,
			"jobs":          jobs,
			"episode_count": NullableInt(cr.EpisodeCount),
			"person":        Person(person),
		})
	}

	crew := make(map[string]any, len(crewByDepartment))
	for department, members := range crewByDepartment {
		crew[department] = members
	}

	return map[string]any{"cast": cast, "crew": crew}
}

// crewDepartment maps TMDB's department label onto the contract's crewPosition
// enum. An unknown label is grouped under "crew" rather than emitted as a value
// a conforming client would reject.
func crewDepartment(department string) string {
	switch strings.ToLower(strings.TrimSpace(department)) {
	case "acting", "actors":
		return "acting"
	case "production":
		return "production"
	case "art":
		return "art"
	case "costume & make-up":
		return "costume & make-up"
	case "directing":
		return "directing"
	case "writing":
		return "writing"
	case "sound":
		return "sound"
	case "camera":
		return "camera"
	case "lighting":
		return "lighting"
	case "visual effects":
		return "visual effects"
	case "editing":
		return "editing"
	case "creator":
		return "creator"
	case "created by":
		return "created by"
	default:
		return "crew"
	}
}
