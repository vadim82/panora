package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/playback"
)

func TestRefreshHealthOutputDescribesUnknownEligibility(t *testing.T) {

	providers := []playback.ProviderRefreshHealth{{ID: "wallhaven", Enabled: true, State: "ready", TotalInputs: 2, OmittedInputs: 1, Inputs: []playback.InputRefreshHealth{{ID: "nature", State: "ready", Interval: "5m0s", Counts: &playback.RefreshCounts{Scope: "committed_metadata_in_attempt", Inspected: 24, Pages: 1}}}}}
	providers[0].Retries = []playback.RetryHealth{{Scope: "provider_metadata", Attempts: 2, NextRetry: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC), Failure: playback.RefreshFailure{Stage: "resolve", Code: "rate_limited"}}}
	out := refreshHealth(providers)
	for _, want := range []string{"Provider wallhaven: ready", "nature: ready", "Inspected metadata: 24", "known eligible: unknown", "1 additional input", "Retry provider_metadata [resolve/rate_limited]", "attempt 2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %s", want, out)
		}
	}
}
