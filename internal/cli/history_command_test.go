package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/playback"
)

func TestHistoryCLIAgainstPrivateService(t *testing.T) {
	options, socket := serviceFixture(t)
	backend := &fakeWallpaperBackend{}
	launchService(t, options, socket, func(context.Context) (desktop.WallpaperBackend, error) { return backend, nil })
	first := cliStatus(t, socket, "next")
	second := cliStatus(t, socket, "next")
	text, err := exclusionCLI(t, "history", "--socket", socket)
	if err != nil || !strings.Contains(text, first.Current.ID) || !strings.Contains(text, "Availability") {
		t.Fatalf("human list=%q %v", text, err)
	}
	raw, err := exclusionCLI(t, "history", "ls", "--limit", "1", "--socket", socket, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var page playback.HistoryPage
	if err := json.Unmarshal([]byte(raw), &page); err != nil || len(page.Entries) != 1 || page.Entries[0].ID != second.Current.ID || page.NextBefore != second.Current.ID {
		t.Fatalf("JSON=%s %v", raw, err)
	}
	raw, err = exclusionCLI(t, "history", "replay", first.Current.ID, "--socket", socket, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var status playback.Status
	if err := json.Unmarshal([]byte(raw), &status); err != nil || status.Current.ID != first.Current.ID || status.HistoryLength != 2 {
		t.Fatalf("replay=%s %v", raw, err)
	}
	for _, args := range [][]string{
		{"history", "replay", "pruned", "--socket", socket, "--json"},
		{"history", "ls", "--limit", "11", "--socket", socket, "--json"},
		{"history", "replay", "--json"},
	} {
		raw, err := exclusionCLI(t, args...)
		var envelope struct {
			Version int
			Error   any
		}
		if err == nil {
			t.Fatalf("invalid command succeeded=%v", args)
		}
		if err := json.Unmarshal([]byte(raw), &envelope); err != nil || envelope.Version != 1 || envelope.Error == nil {
			t.Fatalf("JSON error=%s %v", raw, err)
		}
	}
	if backend.calls != 3 {
		t.Fatalf("calls=%d", backend.calls)
	}
}
