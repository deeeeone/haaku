package tmdb

import (
	"net/url"
	"strconv"
)

// FindResult is the response of TMDB's /find endpoint: the same external id
// looked up across every type TMDB carries. Only the two types haaku ingests
// are typed; the rest are left undeclared (TMDB returns them as arrays, and
// json ignores unknown fields).
//
// This is the SUPPLEMENTARY direction of the TVDB↔TMDB link: TVDB names the
// record, and when its remoteIds carry no TheMovieDB.com entry (or carry a
// stale one) /find is how haaku learns which TMDB record enriches it.
type FindResult struct {
	MovieResults []Movie  `json:"movie_results"`
	TVResults    []Show   `json:"tv_results"`
	TVSeasons    []Season `json:"tv_season_results"`
	PersonResult []Person `json:"person_results"`
}

// Find looks an external id up across TMDB's types. extSource is TMDB's
// vocabulary: "tvdb_id", "imdb_id", "tvdb", ...
func (c *Client) Find(externalID, extSource string) (*FindResult, error) {
	path := "/find/" + url.PathEscape(externalID)
	p := url.Values{}
	p.Set("external_source", extSource)
	resp, err := c.doV3("GET", path, p)
	if err != nil {
		return nil, err
	}
	return decode[FindResult](resp)
}

// MovieIDByTVDBID resolves a TVDB movie id to a TMDB movie id, or 0 when TMDB
// carries no matching movie. It is the fallback used when a TVDB record has no
// TheMovieDB.com remote id.
func (c *Client) MovieIDByTVDBID(tvdbID int) (int, error) {
	if tvdbID <= 0 {
		return 0, nil
	}
	res, err := c.Find(strconv.Itoa(tvdbID), "tvdb_id")
	if err != nil {
		return 0, err
	}
	if len(res.MovieResults) == 0 {
		return 0, nil
	}
	return res.MovieResults[0].ID, nil
}

// ShowIDByTVDBID resolves a TVDB series id to a TMDB TV id, or 0 when TMDB
// carries no matching show.
func (c *Client) ShowIDByTVDBID(tvdbID int) (int, error) {
	if tvdbID <= 0 {
		return 0, nil
	}
	res, err := c.Find(strconv.Itoa(tvdbID), "tvdb_id")
	if err != nil {
		return 0, err
	}
	if len(res.TVResults) == 0 {
		return 0, nil
	}
	return res.TVResults[0].ID, nil
}
