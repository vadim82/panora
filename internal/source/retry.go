package source

import (
	"net/http"
	"strconv"
	"time"
)

// RetryAfter accepts HTTP delay seconds or a date, rounds dates up, and bounds
// hints to the provider protocol's one-day limit. Invalid values have no hint.
func RetryAfter(value string, now time.Time) *int64 {
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		when, dateErr := http.ParseTime(value)
		if dateErr != nil {
			return nil
		}
		delay := when.Sub(now)
		seconds = 0
		if delay > 0 {
			seconds = int64(delay / time.Second)
			if delay%time.Second != 0 {
				seconds++
			}
		}
	} else if seconds < 0 {
		return nil
	}
	seconds = min(seconds, 86400)
	return &seconds
}
