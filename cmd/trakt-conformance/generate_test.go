package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/haaku-tv/haaku/internal/contract"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/parity"
)

// fixturePair returns two fixture sets whose every id differs, so a case that
// kept a literal would produce the same request for both.
func fixturePair() (Fixtures, Fixtures) {
	base := Fixtures{
		MovieID: 11, Movie2ID: 12, MovieTMDB: 550, MovieIMDB: "tt550", MovieSlug: "movie-a", MovieTVDB: 1022,
		ShowID: 21, ShowTMDB: 1396, ShowIMDB: "tt1396", ShowSlug: "show-a", ShowTVDB: 81189,
		SeasonID: 31, SeasonNum: 1, EpisodeID: 41, Episode2ID: 42, EpisodeNum: 1,
		PersonID: 51, PersonTMDB: 17419, PersonIMDB: "nm17419", PersonSlug: "person-a",
		ListID: 61, ListSlug: "list-a", ListItemID: 62, SmartListID: 63,
		CommentID: 71, ThreadCommentID: 72, NoteID: 73,
		UserID: "viewer-a", Username: "viewer-a", Email: "a@example.test", Password: "pw",
		TargetUserID: "target-a", TargetUsername: "target-a",
		AppClientID: "11111111-1111-1111-1111-111111111111", AppClientSecret: "secret-a",
		RedirectURI: "https://a.example/cb", GrantCode: "grant-a",
		CodeVerifier: "verifier-a", CodeChallenge: "challenge-a", DeviceCode: "device-a",
	}
	other := base
	other.MovieID, other.Movie2ID, other.MovieTMDB = 911, 912, 680
	other.MovieIMDB, other.MovieSlug, other.MovieTVDB = "tt680", "movie-b", 2022
	other.ShowID, other.ShowTMDB = 921, 1397
	other.ShowIMDB, other.ShowSlug, other.ShowTVDB = "tt1397", "show-b", 91189
	other.SeasonID, other.EpisodeID, other.Episode2ID = 931, 941, 942
	other.PersonID, other.PersonTMDB, other.PersonIMDB, other.PersonSlug = 951, 27419, "nm27419", "person-b"
	other.ListID, other.ListItemID, other.SmartListID = 961, 962, 963
	other.CommentID, other.ThreadCommentID, other.NoteID = 971, 972, 973
	other.UserID, other.TargetUserID = "viewer-b", "target-b"
	other.FollowUserID, other.BlockUserID = "follow-b", "block-b"
	other.CommentID, other.ThreadCommentID, other.DeleteCommentID = 971, 972, 974
	other.NoteID, other.DeleteNoteID = 973, 975
	other.AppClientID, other.GrantCode, other.CodeVerifier, other.CodeChallenge, other.DeviceCode =
		"22222222-2222-2222-2222-222222222222", "grant-b", "verifier-b", "challenge-b", "device-b"
	return base, other
}

// TestCasesFollowFixtureIDs is the seed/case coupling test issue #113 asks for:
// change every seeded id and the generated request changes with it. A case
// pinned to a lucky constant (id 1, tmdb 550, "conformance" client) fails here.
func TestCasesFollowFixtureIDs(t *testing.T) {
	doc, err := contract.Load("../../docs/parity/upstream/trakt-openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	a, b := fixturePair()

	cases := []struct {
		op   string
		ours string
	}{
		{"GET /movies/{id}", "GET /v1/movies/{id}"},
		{"GET /shows/{id}", "GET /v1/shows/{id}"},
		{"GET /people/{id}/movies", "GET /v1/people/{id}/movies"},
		{"GET /notes/{id}", "GET /v1/notes/{id}"},
		{"GET /lists/{id}", "GET /v1/lists/{id}"},
		{"GET /smart-lists/{list_id}", "GET /v1/smart-lists/{list_id}"},
		{"GET /comments/{id}/replies", "GET /v1/comments/{id}/replies"},
		{"DELETE /users/{id}/follow", "DELETE /v1/users/{id}/follow"},
		{"POST /users/{id}/block", "POST /v1/users/{id}/block"},
		{"GET /search/{id_type}/{id}", "GET /v1/search/{id_type}/{id}"},
		{"POST /checkin", "POST /v1/checkin"},
		{"POST /sync/collection", "POST /v1/sync/collection"},
		{"POST /oauth/device/token", "POST /oauth/device/token"},
		{"POST /oauth/token", "POST /oauth/token"},
	}
	for _, tc := range cases {
		op, ok := doc.Operation(tc.op[:strings.IndexByte(tc.op, ' ')], tc.op[strings.IndexByte(tc.op, ' ')+1:])
		if !ok {
			t.Fatalf("%s: not in the contract", tc.op)
		}
		row := parity.Row{Trakt: tc.op, Ours: tc.ours}
		ca := generateCase(doc, op, row, routeMap(tc.ours), a)
		cb := generateCase(doc, op, row, routeMap(tc.ours), b)
		if ca.NoCaseReason != "" {
			t.Fatalf("%s: no case: %s", tc.op, ca.NoCaseReason)
		}
		reqA := ca.Method + " " + ca.Path + "?" + ca.Query.Encode() + " " + string(ca.Body)
		reqB := cb.Method + " " + cb.Path + "?" + cb.Query.Encode() + " " + string(cb.Body)
		if reqA == reqB {
			t.Errorf("%s: request did not change when the fixture ids changed: %s", tc.op, reqA)
		}
	}

	// The body's tmdb id is the seeded one, not a literal.
	op, _ := doc.Operation("POST", "/sync/collection")
	ca := generateCase(doc, op, parity.Row{Ours: "POST /v1/sync/collection"}, routeMap("POST /v1/sync/collection"), a)
	var body map[string]any
	if err := json.Unmarshal(ca.Body, &body); err != nil {
		t.Fatalf("sync body: %v (%s)", err, ca.Body)
	}
	if !strings.Contains(string(ca.Body), `"tmdb":550`) {
		t.Errorf("sync collection body does not carry the seeded movie tmdb id: %s", ca.Body)
	}
	if !strings.Contains(string(ca.Body), `"tmdb":1396`) {
		t.Errorf("sync collection body does not carry the seeded show tmdb id: %s", ca.Body)
	}
}

// A route map with the one route under test; auth is irrelevant to generation.
func routeMap(path string) map[string]openapi.RouteDef {
	method, p := path[:strings.IndexByte(path, ' ')], path[strings.IndexByte(path, ' ')+1:]
	return map[string]openapi.RouteDef{strings.ToUpper(method) + " " + p: {Auth: "none"}}
}

func TestClassify(t *testing.T) {
	cases := map[string]string{
		"missing header X-Pagination-Page":       "header",
		"status 200 is not a contract 2xx [204]": "status",
		"$.title: missing required field":        "field",
		"$: expected array, got object":          "field",
		"response is not JSON: boom":             "envelope",
		"empty array: nothing to validate":       "empty",
	}
	for msg, want := range cases {
		if got := classify(msg); got != want {
			t.Errorf("classify(%q) = %q, want %q", msg, got, want)
		}
	}
}

// TestPathsUseContractEnums pins the request-side fix for the two shapes that
// used to be sent with a handler-invalid literal: search/{type} was "movies"
// and the comment_type segment was "shows".
func TestPathsUseContractEnums(t *testing.T) {
	doc, err := contract.Load("../../docs/parity/upstream/trakt-openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	fx, _ := fixturePair()
	for _, tc := range []struct{ op, ours, want string }{
		{"GET /search/{type}", "GET /v1/search/{type}", "/v1/search/movie"},
		{"GET /users/{id}/comments/{comment_type}/{type}", "GET /v1/users/{id}/comments/{comment_type}/{type}", "/v1/users/viewer-a/comments/all/"},
	} {
		op, ok := doc.Operation(tc.op[:strings.IndexByte(tc.op, ' ')], tc.op[strings.IndexByte(tc.op, ' ')+1:])
		if !ok {
			t.Fatalf("%s: not in the contract", tc.op)
		}
		c := generateCase(doc, op, parity.Row{Ours: tc.ours}, routeMap(tc.ours), fx)
		if c.NoCaseReason != "" {
			t.Fatalf("%s: no case: %s", tc.op, c.NoCaseReason)
		}
		if !strings.HasPrefix(c.Path, tc.want) {
			t.Errorf("%s: path = %q, want prefix %q", tc.op, c.Path, tc.want)
		}
	}
}
