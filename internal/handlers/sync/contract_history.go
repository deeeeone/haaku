package sync

import (
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/response"
)

// contractHistoryItems renders a single-item history read in the contract's
// row shape. The contract requires id, watched_at, action and type; our own
// fields stay on the object so existing consumers keep working.
func contractHistoryItems(items []database.UserHistory) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		h := &items[i]
		row := map[string]any{
			"id":         h.ID,
			"media_type": h.MediaType,
			"media_id":   h.MediaID,
			"watched_at": response.FormatTime(h.WatchedAt),
			"action":     historyAction(h.Source),
			"type":       h.MediaType,
		}
		if h.SeasonID != nil {
			row["season_id"] = *h.SeasonID
		}
		if h.EpisodeID != nil {
			row["episode_id"] = *h.EpisodeID
		}
		if h.Source != "" {
			row["source"] = h.Source
		}
		out = append(out, row)
	}
	return out
}

// normalizeHistoryType accepts Trakt's plural type segment as well as our
// singular media_type, so one handler serves both /v1/sync/history/movies/1
// and /v1/sync/history/movie/1 without changing what the singular path means.
func normalizeHistoryType(raw string) string {
	switch raw {
	case "movies":
		return "movie"
	case "shows":
		return "show"
	case "episodes":
		return "episode"
	case "seasons":
		return "season"
	default:
		return raw
	}
}

// historyAction maps our row source onto Trakt's action vocabulary.
func historyAction(source string) string {
	switch source {
	case "scrobble":
		return "scrobble"
	case "checkin":
		return "checkin"
	default:
		return "watch"
	}
}
