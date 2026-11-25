package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vadim82/panora/internal/control"
)

func humanDuration(value string) string {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return terminalText(value)
	}
	text := duration.String()
	if duration >= time.Minute && duration%time.Minute == 0 {
		text = strings.TrimSuffix(text, "0s")
		if duration%time.Hour == 0 {
			text = strings.TrimSuffix(text, "0m")
		}
	}
	return text
}

func writeControlStatus(cmd *cobra.Command, status control.Status, jsonOutput bool) error {
	if jsonOutput {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(status)
	}
	var text strings.Builder
	out := &text
	state := "running"
	if status.Paused {
		state = "paused"
	}
	if status.SetupRequired {
		state = "waiting for setup"
	}
	fmt.Fprintf(out, "Playback: %s (every %s)\n", state, humanDuration(status.Interval))
	if status.SetupRequired {
		fmt.Fprintln(out, "Setup: add or enable a wallpaper source. No wallpaper changes are scheduled.")
	}
	if status.Current == nil {
		fmt.Fprintln(out, "Last accepted image: none")
	} else {
		fmt.Fprintf(out, "Last accepted image: %s (source: %s)\n", terminalText(status.Current.Path), terminalText(status.Current.SourceID))
	}
	if status.OmarchyThemeName != "" {
		fmt.Fprintf(out, "Active Omarchy theme: %s\n", terminalText(status.OmarchyThemeName))
	}
	if status.OmarchyThemeIssue != "" {
		fmt.Fprintln(out, terminalText(status.OmarchyThemeIssue))
	}
	if status.NextAt != nil {
		fmt.Fprintf(out, "Next scheduled attempt: %s\n", status.NextAt.Local().Format(time.RFC3339))
	}
	if len(status.Providers) > 0 {
		counts := make(map[string]int)
		for _, provider := range status.Providers {
			counts[provider.State]++
		}
		var summary []string
		for _, state := range []string{"failed", "refreshing", "pending", "ready", "empty", "disabled"} {
			if count := counts[state]; count > 0 {
				summary = append(summary, fmt.Sprintf("%d %s", count, state))
			}
		}
		if len(summary) == 0 {
			summary = append(summary, "health unknown")
		}
		fmt.Fprintf(out, "Providers: %s\n", strings.Join(summary, ", "))
	} else if status.Refreshing {
		fmt.Fprintln(out, "Providers: discovery in progress")
	}
	verbose, _ := cmd.Flags().GetBool("verbose")
	if verbose {
		fmt.Fprintf(out, "Busy: %t\nRequested backend: %s\nHistory: %d entries (cursor %d)\n", status.Busy, terminalText(status.Backend), status.HistoryLength, status.HistoryCursor)
		out.WriteString(refreshHealth(status.Providers))
	} else if status.Busy {
		fmt.Fprintln(out, "A wallpaper action is in progress.")
	}
	out.WriteString(statusWarnings(cmd.OutOrStdout(), status))
	_, err := io.WriteString(cmd.OutOrStdout(), text.String())
	return err
}

func writeActionResult(cmd *cobra.Command, status control.Status, action string, jsonOutput bool) error {
	if jsonOutput {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(status)
	}
	message := ""
	switch action {
	case "next", "previous", "replay":
		if status.Current == nil {
			return fmt.Errorf("service returned no accepted image after %s", action)
		}
		message = "Wallpaper request accepted: " + terminalText(status.Current.Path)
	case "pause", "resume":
		message = "Rotation resumed."
		if status.Paused {
			message = "Rotation paused."
		}
	case "exclude":
		message = "Current image excluded from future playback."
	case "restore-exclusion":
		message = "Exclusion removed."
	}
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), message); err != nil {
		return err
	}
	if action == "exclude" {
		status.CurrentExcluded = false // The confirmation already explains this state.
	}
	_, err := io.WriteString(cmd.OutOrStdout(), statusWarnings(cmd.OutOrStdout(), status))
	return err
}

func statusWarnings(out io.Writer, status control.Status) string {
	var text strings.Builder
	var warnings []string
	if status.Uncertain {
		warnings = append(warnings, "Application outcome uncertain: inspect the desktop before another action.")
	}
	if status.CurrentExcluded {
		warnings = append(warnings, "Current image is excluded from future playback.")
	}
	if status.LastError != nil {
		warnings = append(warnings, fmt.Sprintf("Last error [%s]: %s", status.LastError.Code, status.LastError.Message))
	}
	warnings = append(warnings, status.Warnings...)
	warnings = append(warnings, status.RefreshWarnings...)
	if hasProviderRetries(status) {
		warnings = append(warnings, "Provider backoff is active; run panora status --verbose for retry details.")
	}
	seen := make(map[string]bool)
	prefix := styled(out, "33", "Warning:")
	for _, warning := range warnings {
		if seen[warning] {
			continue
		}
		seen[warning] = true
		fmt.Fprintf(&text, "%s %s\n", prefix, terminalText(warning))
	}
	return text.String()
}

func hasProviderRetries(status control.Status) bool {
	for _, provider := range status.Providers {
		if len(provider.Retries) > 0 {
			return true
		}
		for _, input := range provider.Inputs {
			if len(input.Retries) > 0 {
				return true
			}
		}
	}
	return false
}
