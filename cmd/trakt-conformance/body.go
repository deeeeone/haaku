package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// sampleBody synthesises a body that conforms to the contract's request schema,
// using the seeded fixture ids so the items resolve to real rows.
func sampleBody(schema map[string]any, opPath string, fx Fixtures) []byte {
	v := sampleValue(schema, opPath, fx, "", "")
	v = decorateOAuthBody(opPath, v, fx)
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

// decorateOAuthBody adds the fields our OAuth handlers require under names the
// contract does not use. The contract calls the device code `code`; our device
// endpoint reads `device_code`, and both token endpoints branch on an explicit
// `grant_type`. The contract schemas do not forbid additional properties, so
// the body stays contract-valid, and the grant itself is real (seeded client,
// real device code created through /oauth/device/code).
func decorateOAuthBody(opPath string, v any, fx Fixtures) any {
	m := asMap(v)
	if m == nil {
		return v
	}
	switch opPath {
	// Deliberately no case for /oauth/device/token: the contract's body there is
	// {code, client_id, client_secret}. The harness used to add device_code and
	// grant_type, which the contract does not declare, so it was sending a body
	// the contract never defined - and a handler that answered it proved nothing
	// about the operation being tested. sampleValue already fills `code` with the
	// device code the harness obtained over HTTP.
	case "/oauth/token":
		if _, ok := m["grant_type"]; !ok {
			m["grant_type"] = "authorization_code"
		}
		if _, ok := m["code_verifier"]; !ok {
			m["code_verifier"] = fx.CodeVerifier
		}
	}
	return m
}

func sampleValue(schema map[string]any, opPath string, fx Fixtures, prop, media string) any {
	if schema == nil {
		return nil
	}
	if all, ok := schema["allOf"].([]any); ok {
		merged := map[string]any{}
		for _, sub := range all {
			if m := asMap(sub); m != nil {
				mergeSchema(merged, m)
			}
		}
		if len(merged) > 0 {
			return sampleValue(merged, opPath, fx, prop, media)
		}
	}
	// The contract's request bodies express "one of movie/episode/show" as
	// oneOf. Pick an alternative the seeded store can satisfy.
	for _, key := range []string{"oneOf", "anyOf"} {
		if subs, ok := schema[key].([]any); ok && len(subs) > 0 {
			if isIDSet(subs) {
				return sampleValue(pickIDAlternative(subs), opPath, fx, prop, media)
			}
			chosen := sampleValue(pickObjectAlternative(subs), opPath, fx, prop, media)
			// An allOf may merge a block of shared properties with a oneOf of
			// target objects — POST /comments/ is {comment, spoiler, gif} plus
			// oneOf{movie|show|season|episode|list}. Returning only the chosen
			// alternative drops the shared required fields and sends a body the
			// contract never defined, so merge the shared properties in as well.
			if props, ok := schema["properties"].(map[string]any); ok && len(props) > 0 {
				base := map[string]any{}
				names := make([]string, 0, len(props))
				for name := range props {
					names = append(names, name)
				}
				sort.Strings(names)
				for _, name := range names {
					base[name] = sampleValue(asMap(props[name]), opPath, fx, name, mediaForProperty(name, media))
				}
				for k, v := range asMap(chosen) {
					base[k] = v
				}
				return base
			}
			return chosen
		}
	}
	// A string with a documented enum must carry one of its own values.
	if enum, ok := schema["enum"].([]any); ok && len(enum) > 0 {
		for _, e := range enum {
			if e != nil {
				return e
			}
		}
	}
	types := schemaTypesOf(schema)
	// Prefer a concrete type over null.
	use := ""
	for _, t := range types {
		if t != "null" {
			use = t
			break
		}
	}
	switch use {
	case "object":
		out := map[string]any{}
		props := asMap(schema["properties"])
		names := make([]string, 0, len(props))
		for name := range props {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			ps := asMap(props[name])
			childMedia := mediaForProperty(name, media)
			out[name] = sampleValue(ps, opPath, fx, name, childMedia)
		}
		if len(out) == 0 {
			return map[string]any{}
		}
		return out
	case "array":
		items := asMap(schema["items"])
		if items == nil {
			return []any{}
		}
		return []any{sampleValue(items, opPath, fx, prop, mediaForProperty(prop, media))}
	case "integer", "number":
		return sampleInt(prop, fx, media)
	case "boolean":
		return true
	case "string":
		return sampleString(prop, opPath, fx, media)
	}
	if props := asMap(schema["properties"]); len(props) > 0 {
		out := map[string]any{}
		for name, ps := range props {
			out[name] = sampleValue(asMap(ps), opPath, fx, name, mediaForProperty(name, media))
		}
		return out
	}
	return nil
}

// mediaForProperty tracks which media type the enclosing object describes, so
// ids.tmdb inside `show` is the show's tmdb id and not the movie's.
func mediaForProperty(name, current string) string {
	switch name {
	case "movie", "movies":
		return "movie"
	case "show", "shows":
		return "show"
	case "season", "seasons":
		return "season"
	case "episode", "episodes":
		return "episode"
	case "person", "people":
		return "person"
	}
	return current
}

func mergeSchema(dst, src map[string]any) {
	for k, v := range src {
		if k == "properties" {
			if p, ok := v.(map[string]any); ok {
				existing, _ := dst["properties"].(map[string]any)
				if existing == nil {
					existing = map[string]any{}
				}
				for name, ps := range p {
					existing[name] = ps
				}
				dst["properties"] = existing
				continue
			}
		}
		dst[k] = v
	}
}

// isIDSet reports whether every oneOf alternative is an ids object declaring a
// single required id source, e.g. {trakt}|{slug}|{imdb}|{tmdb}|{tvdb}.
func isIDSet(subs []any) bool {
	sources := map[string]bool{"trakt": true, "slug": true, "imdb": true, "tmdb": true, "tvdb": true}
	for _, sub := range subs {
		m := asMap(sub)
		if m == nil {
			return false
		}
		req, _ := m["required"].([]any)
		if len(req) != 1 {
			return false
		}
		name, _ := req[0].(string)
		if !sources[name] {
			return false
		}
	}
	return true
}

// pickIDAlternative prefers the id flavour the media resolver and the seeded
// external_ids rows both support. `trakt` (a local sequence id) is last: it is
// not in external_ids and its meaning collides across media tables.
func pickIDAlternative(subs []any) map[string]any {
	preferred := []string{"tmdb", "imdb", "slug", "tvdb", "trakt"}
	for _, want := range preferred {
		for _, sub := range subs {
			m := asMap(sub)
			req, _ := m["required"].([]any)
			if len(req) == 1 && req[0] == want {
				return m
			}
		}
	}
	return asMap(subs[0])
}

// pickObjectAlternative chooses the branch most likely to resolve in the
// seeded store. A branch that names a `movie` object is preferred (the movie
// fixture is the one every media type can address), then a branch that carries
// an authorization code (the grant this fixture set seeds), then the smallest
// branch.
func pickObjectAlternative(subs []any) map[string]any {
	for _, want := range []string{"movie", "code"} {
		for _, sub := range subs {
			m := asMap(sub)
			if m == nil {
				continue
			}
			if props := asMap(m["properties"]); props != nil {
				if _, ok := props[want]; ok {
					return m
				}
			}
		}
	}
	best := asMap(subs[0])
	bestSize := propertyCount(best)
	for _, sub := range subs[1:] {
		m := asMap(sub)
		if n := propertyCount(m); n < bestSize {
			best, bestSize = m, n
		}
	}
	return best
}

func propertyCount(schema map[string]any) int {
	if props := asMap(schema["properties"]); props != nil {
		return len(props)
	}
	return 1 << 30
}

func mediaTMDB(media string, fx Fixtures) int {
	switch media {
	case "show":
		return fx.ShowTMDB
	case "person":
		return fx.PersonTMDB
	default:
		return fx.MovieTMDB
	}
}

func mediaTVDB(media string, fx Fixtures) int {
	switch media {
	case "show":
		return fx.ShowTVDB
	default:
		return fx.MovieTVDB
	}
}

func mediaIMDB(media string, fx Fixtures) string {
	switch media {
	case "show":
		return fx.ShowIMDB
	case "person":
		return fx.PersonIMDB
	default:
		return fx.MovieIMDB
	}
}

func mediaSlug(media string, fx Fixtures) string {
	switch media {
	case "show":
		return fx.ShowSlug
	case "person":
		return fx.PersonSlug
	default:
		return fx.MovieSlug
	}
}

func mediaLocalID(media string, fx Fixtures) int64 {
	switch media {
	case "show":
		return fx.ShowID
	case "season":
		return fx.SeasonID
	case "episode":
		return fx.EpisodeID
	case "person":
		return fx.PersonID
	default:
		return fx.MovieID
	}
}

// sampleInt fills an integer leaf with the seeded id the field and enclosing
// media type name, so a request that addresses media by tmdb/trakt id resolves
// to a real row.
func sampleInt(prop string, fx Fixtures, media string) int {
	switch prop {
	case "tmdb":
		return mediaTMDB(media, fx)
	case "tvdb":
		return mediaTVDB(media, fx)
	case "trakt", "id":
		return int(mediaLocalID(media, fx))
	case "rating":
		return 9
	case "progress":
		return 50
	case "season":
		return fx.SeasonNum
	case "number":
		return fx.EpisodeNum
	case "number_abs":
		return 1
	case "year":
		return 2008
	}
	return 1
}

// sampleString fills a string leaf with a value the API will accept: an id set
// resolves to the seeded external id for the enclosing media type, and a date
// field gets a real timestamp.
func sampleString(prop, opPath string, fx Fixtures, media string) string {
	switch prop {
	case "ids", "tmdb":
		return fmt.Sprintf("%d", mediaTMDB(media, fx))
	case "imdb":
		return mediaIMDB(media, fx)
	case "tvdb":
		return fmt.Sprintf("%d", mediaTVDB(media, fx))
	case "slug":
		return mediaSlug(media, fx)
	case "watched_at", "collected_at", "rated_at", "listed_at", "favorited_at", "paused_at":
		return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	case "comment", "body", "note", "notes", "message", "name", "title":
		return "conformance"
	case "email":
		return fx.Email
	case "username":
		return fx.Username
	case "password":
		return fx.Password
	case "type":
		if media != "" {
			return media
		}
		return "movies"
	case "privacy":
		return "public"
	case "client_id":
		return fx.AppClientID
	case "client_secret":
		return fx.AppClientSecret
	case "redirect_uri":
		return fx.RedirectURI
	case "code":
		if opPath == "/oauth/device/token" {
			return fx.DeviceCode
		}
		return fx.GrantCode
	case "grant_type":
		if opPath == "/oauth/device/token" {
			return "urn:ietf:params:oauth:grant-type:device_code"
		}
		return "authorization_code"
	case "code_verifier":
		return fx.CodeVerifier
	case "code_challenge":
		return fx.CodeChallenge
	case "token":
		return "conformance-token"
	case "device_code":
		return fx.DeviceCode
	}
	if strings.HasSuffix(prop, "_at") {
		return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	}
	return "conformance"
}

func schemaTypesOf(schema map[string]any) []string {
	switch t := schema["type"].(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, v := range t {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
