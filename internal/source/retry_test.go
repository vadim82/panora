package source

import (
	"net/http"
	"testing"
	"time"
)

func TestRetryAfterDelayDateAndBounds(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 500_000_000, time.UTC)
	for _, tt := range []struct {
		value string
		want  int64
		valid bool
	}{{"20", 20, true}, {"999999", 86400, true}, {"0", 0, true}, {"-1", 0, false}, {"bad", 0, false}, {now.Add(3 * time.Second).Format(http.TimeFormat), 3, true}, {now.Add(-time.Hour).Format(http.TimeFormat), 0, true}, {now.Add(48 * time.Hour).Format(http.TimeFormat), 86400, true}} {
		hint := RetryAfter(tt.value, now)
		if (hint != nil) != tt.valid || hint != nil && *hint != tt.want {
			t.Fatalf("%q: hint=%v want=%d valid=%v", tt.value, hint, tt.want, tt.valid)
		}
	}
}
