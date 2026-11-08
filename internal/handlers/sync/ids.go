package sync

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// idsObj is the external-id set a sync request addresses media by. Every write
// endpoint in this package decodes it, for movies, shows, seasons and episodes
// alike.
type idsObj struct {
	TMDB int    `json:"tmdb"`
	TVDB int    `json:"tvdb,omitempty"`
	IMDB string `json:"imdb,omitempty"`
}

// UnmarshalJSON accepts an id as either a JSON number or a JSON string holding
// that number.
//
// The string form is not optional tolerance — it is the form haaku itself
// serves. Every media response carries `ids` as
// `{"id":1,"slug":"severance","tmdb":95396,"imdb":"tt11280740",
// "tvdb":"371980"}` (TVDB's own ids are strings and
// `external_ids.external_id` is a TEXT column), so a client that keeps the ids
// it was handed and echoes them back into a sync write was answered
// `400 invalid JSON body` — a message that named the JSON syntax, which was
// valid. The web UI does exactly that (search/detail page →
// /v1/media/resolve → watchlist add). Trakt-compatible clients send the same id
// as a number, so both forms have to decode.
//
// The struct keeps int fields, so the not_found echo of a dropped id is
// unchanged.
func (o *idsObj) UnmarshalJSON(data []byte) error {
	// The ids are read as raw JSON first: a numeric field cannot hold the
	// string form, and a string field cannot hold the numeric one.
	var raw struct {
		TMDB json.RawMessage `json:"tmdb"`
		TVDB json.RawMessage `json:"tvdb"`
		IMDB string          `json:"imdb"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var err error
	if o.TMDB, err = decodeFlexibleID(raw.TMDB); err != nil {
		return fmt.Errorf("ids.tmdb: %w", err)
	}
	if o.TVDB, err = decodeFlexibleID(raw.TVDB); err != nil {
		return fmt.Errorf("ids.tvdb: %w", err)
	}
	o.IMDB = raw.IMDB
	return nil
}

// decodeFlexibleID decodes an external id that arrived as a JSON number, a JSON
// string holding one, or nothing at all. Absent and null both mean "not
// supplied" (0), the same value the plain int field decoded them to.
//
// A string that is not a number is an error rather than a silent 0: a
// zeroed id would come back as a mystery entry in `not_found` instead of a 400
// naming the field.
func decodeFlexibleID(raw json.RawMessage) (int, error) {
	if len(raw) == 0 {
		return 0, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		s = strings.TrimSpace(s)
		if s == "" {
			return 0, nil
		}
		n, convErr := strconv.Atoi(s)
		if convErr != nil {
			return 0, fmt.Errorf("%q is not a numeric id", s)
		}
		return n, nil
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, err
	}
	return n, nil
}
