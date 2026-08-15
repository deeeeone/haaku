package response

import (
	"time"
)

// TimeLayout is the API-wide RFC3339 timestamp layout with millisecond
// precision. All handler response timestamps must use FormatTime instead of
// hand-rolled layout literals so the wire format stays consistent.
const TimeLayout = "2006-01-02T15:04:05.000Z"

// FormatTime formats t using TimeLayout (millisecond precision, UTC).
func FormatTime(t time.Time) string {
	return t.Format(TimeLayout)
}
