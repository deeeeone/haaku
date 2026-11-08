package tvdb

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// RemoteIDMatch is one hit of TVDB's remote-id search, flattened from the
// {"series": {...}} / {"movie": {...}} wrapper TVDB puts around each result.
type RemoteIDMatch struct {
	// Type is haaku's media type: "movie" or "show".
	Type  string
	ID    int
	Name  string
	Slug  string
	Image string
	Year  int
}

// SearchByRemoteID asks TVDB which of its records carry an external id — an
// IMDb id ("tt0903747"), a TMDB id, and so on. TVDB answers with one wrapper
// per match whose key states the record type, which is how a caller that only
// holds an IMDb id learns both the TVDB id and whether it is a series or a
// movie.
//
// A single external id can match SEVERAL records: TMDB numbers movies and TV
// shows in the same space, so a tmdb id may be attached to both a series and
// an unrelated movie (measured: tmdb 1396 matches Breaking Bad and a film).
// Callers that know which they want must filter on Type; resolution.go does
// that with the `type` query parameter of /v1/media/resolve.
func (c *Client) SearchByRemoteID(ctx context.Context, remoteID string) ([]RemoteIDMatch, error) {
	remoteID = strings.TrimSpace(remoteID)
	if remoteID == "" {
		return nil, nil
	}

	var out struct {
		Status string `json:"status"`
		Data   []map[string]struct {
			ID     int    `json:"id"`
			Name   string `json:"name"`
			Slug   string `json:"slug"`
			Image  string `json:"image"`
			Year   string `json:"year"`
			Status struct {
				Name string `json:"name"`
			} `json:"status"`
		} `json:"data"`
	}
	path := "/search/remoteid/" + url.PathEscape(remoteID)
	if err := c.do(ctx, "GET", path, nil, nil, &out); err != nil {
		return nil, err
	}

	matches := make([]RemoteIDMatch, 0, len(out.Data))
	for _, entry := range out.Data {
		for key, record := range entry {
			var mediaType string
			switch strings.ToLower(key) {
			case "series":
				mediaType = "show"
			case "movie":
				mediaType = "movie"
			default:
				continue
			}
			if record.ID <= 0 {
				continue
			}
			year, _ := strconv.Atoi(strings.TrimSpace(record.Year))
			matches = append(matches, RemoteIDMatch{
				Type:  mediaType,
				ID:    record.ID,
				Name:  record.Name,
				Slug:  record.Slug,
				Image: record.Image,
				Year:  year,
			})
		}
	}
	return matches, nil
}
