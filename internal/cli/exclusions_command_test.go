package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/playback"
	"github.com/vadim82/panora/internal/sources/local"
)

func exclusionCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := Execute(context.Background(), args, &out, io.Discard)
	return out.String(), err
}

func TestExclusionsCLIAgainstService(t *testing.T) {
	options, socket := serviceFixture(t)
	// Exercise real folder resolution without coupling exclusion assertions to
	// race-instrumented child-process startup. Provider tests cover that boundary.
	options.Request.Source = local.Provider{}
	backend := &fakeWallpaperBackend{}
	launchService(t, options, socket, func(context.Context) (desktop.WallpaperBackend, error) { return backend, nil })
	first := cliStatus(t, socket, "next")
	if status := cliExclusionStatus(t, socket, "add"); !status.CurrentExcluded || !status.ExclusionUndoAvailable || status.Current.Path != first.Current.Path {
		t.Fatalf("exclude: %+v", status)
	}
	text, err := exclusionCLI(t, "exclusions", "--socket", socket)
	if err != nil || !strings.Contains(text, "Provider") || !strings.Contains(text, "home/") {
		t.Fatalf("human list: %q %v", text, err)
	}
	raw, err := exclusionCLI(t, "exclusions", "--socket", socket, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var page playback.ExclusionPage
	if err := json.Unmarshal([]byte(raw), &page); err != nil || len(page.Entries) != 1 {
		t.Fatalf("JSON list: %s %v", raw, err)
	}
	second := cliStatus(t, socket, "next")
	if second.Current.Path == first.Current.Path {
		t.Fatal("CLI selected excluded image")
	}
	if _, err := exclusionCLI(t, "exclusions", "remove", fmt.Sprint(page.Entries[0].ID), "--socket", socket, "--json"); err != nil {
		t.Fatal(err)
	}
	if previous := cliStatus(t, socket, "previous"); previous.Current.Path != first.Current.Path {
		t.Fatal("restore did not restore playback")
	}
	text, err = exclusionCLI(t, "exclusions", "--socket", socket)
	if err != nil || text != "No excluded images.\n" {
		t.Fatalf("empty list: %q %v", text, err)
	}
	for _, args := range [][]string{{"exclusions", "remove", "bad", "--json"}, {"exclusions", "ls", "--limit", "11", "--socket", socket, "--json"}, {"exclusions", "--backend", "macos", "--socket", socket, "--json"}} {
		raw, err := exclusionCLI(t, args...)
		if err == nil {
			t.Fatalf("invalid command succeeded: %v", args)
		}
		var envelope struct {
			Version int `json:"version"`
			Error   any `json:"error"`
		}
		if err := json.Unmarshal([]byte(raw), &envelope); err != nil || envelope.Version != 1 || envelope.Error == nil {
			t.Fatalf("JSON error: %s %v", raw, err)
		}
	}
}

func cliExclusionStatus(t *testing.T, socket, action string) playback.Status {
	t.Helper()
	raw, err := exclusionCLI(t, "exclusions", action, "--socket", socket, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var status playback.Status
	if err := json.Unmarshal([]byte(raw), &status); err != nil {
		t.Fatal(err)
	}
	return status
}
