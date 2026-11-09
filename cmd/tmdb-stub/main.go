// Command tmdb-stub is a local stand-in for the TMDB v3 API used by the
// conformance gate.
//
// The gate must exercise the real provider path — fetch from the provider,
// store the payload, serve the contract shape from our own table — but it runs
// with no network and no usable TMDB credentials. Pointing the client at this
// stub (TMDB_BASE_URL) makes every provider call a real HTTP request against
// canned JSON, so a 2xx from a provider-backed row is evidence about our code
// rather than about TMDB's availability.
//
// It serves only the endpoints this project calls, and answers 404 with TMDB's
// own "not found" body for anything else. It is a test fixture: no auth check,
// no rate limit, no behaviour beyond what the handlers read.
//
// Usage:
//
//	tmdb-stub -addr 127.0.0.1:18091
//	TMDB_BASE_URL=http://127.0.0.1:18091/3 go run ./cmd/api
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:18091", "address to listen on")
	flag.Parse()

	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("tmdb-stub listening on http://%s (base URL http://%s/3)", *addr, *addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

// route is one canned response keyed by a path pattern.
type route struct {
	pattern *regexp.Regexp
	body    string
}

var routes = []route{
	// Videos: movie, show, season, episode.
	{regexp.MustCompile(`^/3/movie/\d+/videos$`), `{"results":[` + videoEntry("movieTrailerKey", "Trailer") + `]}`},
	{regexp.MustCompile(`^/3/tv/\d+/videos$`), `{"results":[` + videoEntry("showTrailerKey", "Trailer") + `]}`},
	{regexp.MustCompile(`^/3/tv/\d+/season/\d+/videos$`), `{"results":[` + videoEntry("seasonTrailerKey", "Teaser") + `]}`},
	{regexp.MustCompile(`^/3/tv/\d+/season/\d+/episode/\d+/videos$`), `{"results":[` + videoEntry("episodeTrailerKey", "Clip") + `]}`},

	// Per-country release dates.
	{regexp.MustCompile(`^/3/movie/\d+/release_dates$`), `{"results":[
		{"iso_3166_1":"US","release_dates":[
			{"certification":"PG-13","type":3,"release_date":"2010-07-16T00:00:00.000Z"},
			{"certification":"","type":4,"release_date":"2010-12-01T00:00:00.000Z"}]},
		{"iso_3166_1":"GB","release_dates":[
			{"certification":"12A","type":2,"release_date":"2010-07-16T00:00:00.000Z"}]}]}`},

	// Translations: movie, show, season, episode.
	{regexp.MustCompile(`^/3/movie/\d+/translations$`), `{"translations":[
		{"iso_639_1":"en","iso_3166_1":"US","data":{"title":"Fixture Movie","overview":"An English overview.","tagline":"A tagline."}},
		{"iso_639_1":"fr","iso_3166_1":"FR","data":{"title":"Film Fixe","overview":"Un resume.","tagline":""}}]}`},
	{regexp.MustCompile(`^/3/tv/\d+/translations$`), `{"translations":[
		{"iso_639_1":"en","iso_3166_1":"US","data":{"name":"Fixture Show","overview":"A show overview."}}]}`},
	{regexp.MustCompile(`^/3/tv/\d+/season/\d+/translations$`), `{"translations":[
		{"iso_639_1":"en","iso_3166_1":"US","data":{"name":"Fixture Season","overview":"A season overview.","tagline":"A season tagline."}},
		{"iso_639_1":"es","iso_3166_1":"ES","data":{"name":"Temporada","overview":"Una temporada.","tagline":""}}]}`},
	{regexp.MustCompile(`^/3/tv/\d+/season/\d+/episode/\d+/translations$`), `{"translations":[
		{"iso_639_1":"en","iso_3166_1":"US","data":{"name":"Fixture Episode","overview":"An episode overview."}}]}`},

	// Season detail. Registered after the season sub-resources above; the
	// anchored patterns keep the two apart.
	{regexp.MustCompile(`^/3/tv/\d+/season/\d+$`), `{"id":3572,"season_number":1,"name":"Season 1","overview":"The first season","air_date":"2008-01-20","poster_path":"/season.jpg","vote_average":8.9,"vote_count":120,"episodes":[]}`},

	// Title details, for production companies and networks.
	{regexp.MustCompile(`^/3/movie/\d+$`), `{"id":550,"title":"Fixture Movie","production_companies":[
		{"id":420,"name":"Warner Bros.","origin_country":"US"},
		{"id":508,"name":"Dune Entertainment","origin_country":"US"}]}`},
	{regexp.MustCompile(`^/3/tv/\d+$`), `{"id":1396,"name":"Fixture Show","networks":[
		{"id":174,"name":"AMC","origin_country":"US"}]}`},

	// Watch providers, for the contract's refresh/justwatch re-fetch.
	{regexp.MustCompile(`^/3/movie/\d+/watch/providers$`), `{"id":550,"results":{"US":{"link":"https://www.themoviedb.org/movie/550/watch?locale=US","flatrate":[{"provider_id":8,"provider_name":"Netflix","logo_path":"/netflix.jpg"}],"rent":[{"provider_id":2,"provider_name":"Apple TV","logo_path":"/apple.jpg"}]}}}`},
	{regexp.MustCompile(`^/3/tv/\d+/watch/providers$`), `{"id":1396,"results":{"US":{"link":"https://www.themoviedb.org/tv/1396/watch?locale=US","flatrate":[{"provider_id":8,"provider_name":"Netflix","logo_path":"/netflix.jpg"}]}}}`},

	// Credits: enough shape for the credit handlers to read.
	{regexp.MustCompile(`^/3/movie/\d+/credits$`), `{"id":550,"cast":[
		{"id":819,"name":"Fixture Actor","character":"Narrator","order":0,"profile_path":"/actor.jpg"}],
		"crew":[{"id":7467,"name":"Fixture Director","job":"Director","department":"Directing"}]}`},
	{regexp.MustCompile(`^/3/tv/\d+/credits$`), `{"id":1396,"cast":[
		{"id":17419,"name":"Fixture Actor","character":"Lead","order":0,"profile_path":"/actor.jpg"}],
		"crew":[{"id":66633,"name":"Fixture Creator","job":"Creator","department":"Writing"}]}`},

	// Static configuration trees some handlers read.
	{regexp.MustCompile(`^/3/configuration/certifications$`), `{"certifications":{"US":[
		{"certification":"PG-13","meaning":"Parents strongly cautioned","order":3},
		{"certification":"R","meaning":"Restricted","order":4}],"GB":[
		{"certification":"12A","meaning":"Suitable for 12 and over","order":3}]}}`},
	// The older per-media certification trees. The reference handler reads these
	// (not the combined configuration tree above), so the catalogue rows for
	// /certifications/movies and /certifications/shows depend on them.
	{regexp.MustCompile(`^/3/certification/movie/list$`), `{"certifications":{"US":[
		{"certification":"PG-13","meaning":"Parents strongly cautioned","order":3},
		{"certification":"R","meaning":"Restricted","order":4}],"GB":[
		{"certification":"12A","meaning":"Suitable for 12 and over","order":3}]}}`},
	{regexp.MustCompile(`^/3/certification/tv/list$`), `{"certifications":{"US":[
		{"certification":"TV-14","meaning":"Parents strongly cautioned","order":5},
		{"certification":"TV-MA","meaning":"Mature audiences","order":6}],"GB":[
		{"certification":"15","meaning":"Suitable only for 15 years and over","order":4}]}}`},
	{regexp.MustCompile(`^/3/configuration/countries$`), `[{"iso_3166_1":"US","english_name":"United States"},{"iso_3166_1":"GB","english_name":"United Kingdom"}]`},
	{regexp.MustCompile(`^/3/configuration/languages$`), `[{"iso_639_1":"en","english_name":"English","name":"English"},{"iso_639_1":"fr","english_name":"French","name":"Francais"}]`},
	{regexp.MustCompile(`^/3/configuration/primary_translations$`), `["en","fr","es"]`},
}

func videoEntry(key, kind string) string {
	return fmt.Sprintf(`{"id":%q,"key":%q,"site":"YouTube","type":%q,"name":"Fixture %s","official":true,"iso_639_1":"en","iso_3166_1":"US","size":1080,"published_at":"2020-01-02T03:04:05.000Z"}`,
		"v1-"+key, key, kind, kind)
}

func handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			_, _ = fmt.Fprint(w, "ok")
			return
		}
		path := strings.TrimSuffix(r.URL.Path, "/")
		for _, rt := range routes {
			if rt.pattern.MatchString(path) {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				_, _ = fmt.Fprint(w, rt.body)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"status_code":34,"status_message":"The resource you requested could not be found."}`)
	})
}
