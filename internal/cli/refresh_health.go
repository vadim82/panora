package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/vadim82/panora/internal/playback"
)

func refreshHealth(providers []playback.ProviderRefreshHealth) string {
	var out strings.Builder
	for _, provider := range providers {
		fmt.Fprintf(&out, "Provider %s: %s (%d inputs)\n", terminalText(provider.ID), terminalText(provider.State), provider.TotalInputs)
		appendRetries(&out, provider.Retries)
		for _, input := range provider.Inputs {
			fmt.Fprintf(&out, "  %s: %s\n", terminalText(input.ID), terminalText(input.State))
			for _, stamp := range []struct {
				label string
				value *time.Time
			}{
				{"Last attempt", input.LastAttempt}, {"Last success", input.LastSuccess}, {"Discovery eligible", input.NextRefresh},
			} {
				if stamp.value != nil {
					fmt.Fprintf(&out, "    %s: %s\n", stamp.label, stamp.value.Format(time.RFC3339))
				}
			}
			if counts := input.Counts; counts != nil {
				eligible := "unknown"
				if counts.KnownEligible != nil {
					eligible = fmt.Sprint(*counts.KnownEligible)
				}
				fmt.Fprintf(&out, "    Inspected metadata: %d (%s, %d pages); known eligible: %s\n", counts.Inspected, counts.Scope, counts.Pages, eligible)
			}
			appendRetries(&out, input.Retries)
			if failure := input.Failure; failure != nil {
				fmt.Fprintf(&out, "    Failure [%s/%s]: %s\n", terminalText(failure.Stage), terminalText(failure.Code), terminalText(failure.Message))
			}
		}
		if provider.OmittedInputs > 0 {
			fmt.Fprintf(&out, "  %d additional input health entries omitted.\n", provider.OmittedInputs)
		}
	}
	return out.String()
}

func appendRetries(out *strings.Builder, retries []playback.RetryHealth) {
	for _, retry := range retries {
		fmt.Fprintf(out, "    Retry %s [%s/%s]: %s (attempt %d, %d affected images)\n", terminalText(retry.Scope), terminalText(retry.Failure.Stage), terminalText(retry.Failure.Code), retry.NextRetry.Format(time.RFC3339), retry.Attempts, retry.AffectedImages)
	}
}
