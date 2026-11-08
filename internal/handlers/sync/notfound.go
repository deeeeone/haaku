package sync

// notFoundList collects ids a sync request could not resolve so the response
// can say which ones were dropped. "added 0" with an empty list left a client
// unable to tell a rejected id from one it had already collected.
type notFoundList struct {
	movies []map[string]interface{}
	shows  []map[string]interface{}
}

func (n *notFoundList) movie(ids interface{}) {
	n.movies = append(n.movies, map[string]interface{}{"ids": ids})
}

func (n *notFoundList) show(ids interface{}) {
	n.shows = append(n.shows, map[string]interface{}{"ids": ids})
}

// payload renders the list as the JSON object the sync responses carry. Empty
// slices stay [] rather than null so clients can iterate without a nil check.
func (n *notFoundList) payload() map[string]interface{} {
	movies, shows := n.movies, n.shows
	if movies == nil {
		movies = []map[string]interface{}{}
	}
	if shows == nil {
		shows = []map[string]interface{}{}
	}
	return map[string]interface{}{"movies": movies, "shows": shows}
}
