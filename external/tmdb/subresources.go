package tmdb

import "fmt"

// Per-item sub-resource fetches (issue #68). These are the dedicated endpoints
// behind Trakt's videos / releases / translations / studios reads. They go
// through the same client, the same HTTP client and the same timeout as every
// other call in this package — there is no second client or retry loop.

// GetMovieVideos returns a movie's trailers, teasers and clips.
func (c *Client) GetMovieVideos(id int) (*Videos, error) {
	resp, err := c.doV3("GET", fmt.Sprintf("/movie/%d/videos", id), nil)
	if err != nil {
		return nil, err
	}
	return decode[Videos](resp)
}

// GetShowVideos returns a show's trailers, teasers and clips.
func (c *Client) GetShowVideos(id int) (*Videos, error) {
	resp, err := c.doV3("GET", fmt.Sprintf("/tv/%d/videos", id), nil)
	if err != nil {
		return nil, err
	}
	return decode[Videos](resp)
}

// GetSeasonVideos returns a season's videos.
func (c *Client) GetSeasonVideos(showID, season int) (*Videos, error) {
	resp, err := c.doV3("GET", fmt.Sprintf("/tv/%d/season/%d/videos", showID, season), nil)
	if err != nil {
		return nil, err
	}
	return decode[Videos](resp)
}

// GetEpisodeVideos returns one episode's videos.
func (c *Client) GetEpisodeVideos(showID, season, episode int) (*Videos, error) {
	resp, err := c.doV3("GET", fmt.Sprintf("/tv/%d/season/%d/episode/%d/videos", showID, season, episode), nil)
	if err != nil {
		return nil, err
	}
	return decode[Videos](resp)
}

// GetMovieReleaseDates returns a movie's per-country release dates.
func (c *Client) GetMovieReleaseDates(id int) (*ReleaseDates, error) {
	resp, err := c.doV3("GET", fmt.Sprintf("/movie/%d/release_dates", id), nil)
	if err != nil {
		return nil, err
	}
	return decode[ReleaseDates](resp)
}

// GetShowTranslations returns every language TMDB holds for a show.
func (c *Client) GetShowTranslations(id int) (*TranslationsResponse, error) {
	resp, err := c.doV3("GET", fmt.Sprintf("/tv/%d/translations", id), nil)
	if err != nil {
		return nil, err
	}
	return decode[TranslationsResponse](resp)
}

// GetSeasonTranslations returns every language TMDB holds for a season.
func (c *Client) GetSeasonTranslations(showID, season int) (*TranslationsResponse, error) {
	resp, err := c.doV3("GET", fmt.Sprintf("/tv/%d/season/%d/translations", showID, season), nil)
	if err != nil {
		return nil, err
	}
	return decode[TranslationsResponse](resp)
}
