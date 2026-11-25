package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/playback"
	"github.com/vadim82/panora/internal/source"
)

func TestPlaybackBridgePaginationAndLostMutationReconciliation(t *testing.T) {
	var mutations, reads atomic.Int32
	path := serveFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/history":
			if r.URL.Query().Get("before") != "older" || r.URL.Query().Get("limit") != "10" {
				t.Errorf("history query=%s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(playback.HistoryPage{Version: 1, Entries: []playback.HistoryEntry{{ID: "retained", ImageID: 7}}, NextBefore: "oldest"})
		case "/v1/exclusions":
			if r.URL.Query().Get("before") != "42" || r.URL.Query().Get("limit") != "10" {
				t.Errorf("exclusions query=%s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(playback.ExclusionPage{Version: 1, Entries: []playback.Exclusion{{ID: 41, Image: "outside-history"}}, NextBefore: 41})
		case "/v1/status":
			reads.Add(1)
			_ = json.NewEncoder(w).Encode(control.Status{Version: 1, Paused: true, HistoryLength: 3})
		case "/v1/actions/next", "/v1/history/retained/replay", "/v1/exclusions/41/restore":
			if r.Method != http.MethodPost {
				t.Errorf("method=%s", r.Method)
			}
			mutations.Add(1)
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = connection.Close()
		default:
			t.Errorf("unexpected endpoint=%s", r.URL.Path)
		}
	}))
	client := &Client{ctx: t.Context(), socket: path}
	history := client.History("older")
	if history.Page == nil || history.Page.NextBefore != "oldest" {
		t.Fatalf("history=%+v", history)
	}
	excluded := client.Exclusions(42)
	if excluded.Page == nil || excluded.Page.Entries[0].Image != "outside-history" {
		t.Fatalf("exclusions=%+v", excluded)
	}
	for i, action := range []func() Snapshot{
		func() Snapshot { return client.Action("next") },
		func() Snapshot { return client.ReplayHistory("retained") },
		func() Snapshot { return client.RestoreExclusion(41) },
	} {
		result := action()
		if !result.Connected || result.Status == nil || !result.Status.Paused || result.Status.HistoryLength != 3 || result.Code != "request_failed" || !strings.Contains(result.Error, "may have completed") {
			t.Fatalf("reconciliation=%+v", result)
		}
		if want := int32(i + 1); mutations.Load() != want || reads.Load() != want {
			t.Fatalf("mutations=%d reads=%d; each action should reconcile once without replay", mutations.Load(), reads.Load())
		}
	}
}

func TestOpenHistoryUsesFreshRetainedOriginAndBoundsReads(t *testing.T) {
	for _, tc := range []struct {
		name, page, target string
		match              bool
		want               bool
	}{
		{"safe original", "https://wallhaven.cc/w/abc123", "page", true, true},
		{"unsafe origin", "https://secret@example.com/image", "page", true, false},
		{"pruned handle", "https://wallhaven.cc/w/abc123", "page", false, false},
		{"arbitrary target", "https://wallhaven.cc/w/abc123", "/tmp/file", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var reads atomic.Int32
			path := serveFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/history" || r.Method != http.MethodGet {
					t.Errorf("unexpected request=%s %s", r.Method, r.URL.Path)
				}
				reads.Add(1)
				entryID := "other"
				if tc.match {
					entryID = "retained"
				}
				_ = json.NewEncoder(w).Encode(playback.HistoryPage{Version: 1, Entries: []playback.HistoryEntry{{ID: entryID, ImageID: 7, OriginalPath: "/cache/never-use", Attribution: &source.Attribution{PageURL: tc.page}}}, NextBefore: "repeated"})
			}))
			opened := ""
			client := &Client{ctx: t.Context(), socket: path, opener: func(_ context.Context, target string) error { opened = target; return nil }}
			result := client.OpenHistory("retained", 7, tc.target)
			if result.Opened != tc.want || tc.want && opened != tc.page {
				t.Fatalf("result=%+v opened=%q", result, opened)
			}
			if reads.Load() > 2 {
				t.Fatalf("repeated cursor was not bounded: %d", reads.Load())
			}
			if result := client.OpenHistory(strings.Repeat("x", 129), 7, "page"); result.Code != "invalid_request" {
				t.Fatalf("oversized handle=%+v", result)
			}
		})
	}
}
