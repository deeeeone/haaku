package tests

// The ingest half of issue #68: the TMDB sub-resources are stored in the
// title's metadata blob, already in the contract shape the read paths serve.

import (
	"testing"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/internal/metadata"
)

// numberOf reads a stored number. The metadata blob round-trips through JSONB,
// so an int written by the builder comes back as a float64.
func numberOf(t *testing.T, value any) float64 {
	t.Helper()
	switch n := value.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float64:
		return n
	default:
		t.Fatalf("value %v (%T) is not a number", value, value)
		return 0
	}
}

func TestIngestMovieStoresVideosStudiosAndReleases(t *testing.T) {
	db := setupProviderTestDB(t)

	movie := sampleMovie()
	movie.ProductionCompanies = []tmdb.ProductionCompany{
		{ID: 79, Name: "Village Roadshow Pictures", OriginCountry: "US"},
	}
	movie.Videos = &tmdb.Videos{Results: []tmdb.Video{
		{Key: "FVI84Dfx2-I", Site: "YouTube", Type: "Trailer", Name: "Trailer", Official: true,
			Size: 1080, PublishedAt: "2019-07-18T16:00:00.000Z", ISO639: "en", ISO3166: "US"},
		{Key: "unsupported", Site: "SomeSite", Type: "Clip", Name: "Other"},
	}}
	movie.ReleaseDates = &tmdb.ReleaseDates{Results: []tmdb.CountryReleaseDates{
		{ISO3166: "US", ReleaseDates: []tmdb.ReleaseDate{
			{Certification: "R", Type: 3, ReleaseDate: "1999-03-31T00:00:00.000Z"},
		}},
	}}

	ingested, err := metadata.IngestMovie(db, movie, nil)
	if err != nil {
		t.Fatalf("IngestMovie: %v", err)
	}

	videos := metadata.StoredObjects(ingested.Metadata, metadata.KeyVideos)
	if len(videos) != 1 {
		t.Fatalf("stored videos = %v, want the one supported site", videos)
	}
	if videos[0]["url"] != "https://youtube.com/watch?v=FVI84Dfx2-I" || videos[0]["type"] != "trailer" {
		t.Fatalf("stored video = %v", videos[0])
	}
	if numberOf(t, videos[0]["size"]) != 1080 || videos[0]["official"] != true {
		t.Fatalf("stored video size/official = %v/%v", videos[0]["size"], videos[0]["official"])
	}

	studios := metadata.StoredObjects(ingested.Metadata, metadata.KeyStudios)
	if len(studios) != 1 || studios[0]["name"] != "Village Roadshow Pictures" {
		t.Fatalf("stored studios = %v", studios)
	}
	ids, _ := studios[0]["ids"].(map[string]any)
	if numberOf(t, ids["tmdb"]) != 79 || ids["slug"] != "village-roadshow-pictures" {
		t.Fatalf("stored studio ids = %v", ids)
	}

	releases := metadata.StoredObjects(ingested.Metadata, metadata.KeyReleases)
	if len(releases) != 1 {
		t.Fatalf("stored releases = %v", releases)
	}
	if releases[0]["country"] != "us" || releases[0]["release_type"] != "theatrical" {
		t.Fatalf("stored release = %v", releases[0])
	}
	if releases[0]["certification"] != "R" {
		t.Fatalf("stored release certification = %v", releases[0]["certification"])
	}
}

func TestIngestShowStoresVideosStudiosAndCertifications(t *testing.T) {
	db := setupProviderTestDB(t)

	show := sampleShow()
	show.ProductionCompanies = []tmdb.ProductionCompany{
		{ID: 1405, Name: "Sony Pictures Television", OriginCountry: "US"},
	}
	show.ContentRatings = &tmdb.ContentRatings{Results: []tmdb.ContentRating{
		{ISO3166: "US", Rating: "TV-MA"},
		{ISO3166: "GB", Rating: "18"},
		{ISO3166: "FR", Rating: ""},
	}}

	ingested, err := metadata.IngestShow(db, show, nil, nil)
	if err != nil {
		t.Fatalf("IngestShow: %v", err)
	}

	if videos := metadata.StoredObjects(ingested.Metadata, metadata.KeyVideos); len(videos) != 1 {
		t.Fatalf("stored videos = %v", videos)
	}
	if studios := metadata.StoredObjects(ingested.Metadata, metadata.KeyStudios); len(studios) != 1 {
		t.Fatalf("stored studios = %v", studios)
	}
	certifications := metadata.StoredObjects(ingested.Metadata, metadata.KeyCertifications)
	if len(certifications) != 2 {
		t.Fatalf("stored certifications = %v, want the two non-empty ratings", certifications)
	}
	if certifications[0]["country"] != "us" || certifications[0]["certification"] != "TV-MA" {
		t.Fatalf("certifications[0] = %v", certifications[0])
	}
}
