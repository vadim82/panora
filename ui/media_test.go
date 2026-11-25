package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/playback"
	"github.com/vadim82/panora/internal/source"
)

func TestCurrentMetadataUsesStatusWithoutPagingHistory(t *testing.T) {
	current := playback.Entry{ID: strings.Repeat("a", 32), ImageID: 17, Path: "/cache/hash.png"}
	path := serveFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/v1/status" || r.Method != http.MethodGet {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		_ = json.NewEncoder(w).Encode(control.Status{Version: 1, Current: &current, HistoryCursor: 0, HistoryLength: 20,
			CurrentImage: &playback.CurrentImage{Provider: "local", OriginalPath: "/Pictures/original.png", Dimensions: &source.Dimensions{Width: 1920, Height: 1080}}})
	}))
	client := &Client{ctx: t.Context(), socket: path}
	got := client.Status()
	if !got.Connected || got.Current == nil || got.Current.ID != current.ID || got.Current.ImageID != 17 || got.Current.OriginalPath != "/Pictures/original.png" || got.Current.Dimensions == nil || got.Current.Dimensions.Width != 1920 {
		t.Fatalf("snapshot=%+v details=%+v", got, got.Current)
	}
}

func TestCurrentMetadataFailurePreservesConnectedAcceptedStatus(t *testing.T) {
	path := serveFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/status" {
			_ = json.NewEncoder(w).Encode(control.Status{Version: 1, Current: &playback.Entry{ID: "accepted", ImageID: 7}})
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	}))
	client := &Client{ctx: t.Context(), socket: path}
	got := client.Status()
	if !got.Connected || got.Status.Current.ID != "accepted" || got.Current != nil || got.CurrentError != "Current image details are unavailable. Refresh status to try again." || got.Error != "" {
		t.Fatalf("snapshot=%+v", got)
	}
}

func TestPreviewValidatesIdentityPNGAndBounds(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 1))); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		preview playback.ImagePreview
		valid   bool
	}{
		{"valid", playback.ImagePreview{Version: 1, ImageID: 7, Available: true, MIMEType: "image/png", Width: 2, Height: 1, Data: data.Bytes()}, true},
		{"missing", playback.ImagePreview{Version: 1, ImageID: 7, Reason: "not_cached"}, true},
		{"wrong identity", playback.ImagePreview{Version: 1, ImageID: 8, Available: true, MIMEType: "image/png", Width: 2, Height: 1, Data: data.Bytes()}, false},
		{"wrong type", playback.ImagePreview{Version: 1, ImageID: 7, Available: true, MIMEType: "image/svg+xml", Width: 2, Height: 1, Data: data.Bytes()}, false},
		{"wrong dimensions", playback.ImagePreview{Version: 1, ImageID: 7, Available: true, MIMEType: "image/png", Width: 3, Height: 1, Data: data.Bytes()}, false},
		{"invalid bytes", playback.ImagePreview{Version: 1, ImageID: 7, Available: true, MIMEType: "image/png", Width: 2, Height: 1, Data: []byte("not a png")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := serveFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/v1/images/7/preview" {
					t.Errorf("unexpected preview request %s %s", r.Method, r.URL.Path)
				}
				_ = json.NewEncoder(w).Encode(tc.preview)
			}))
			client := &Client{ctx: t.Context(), socket: path}
			got := client.Preview(7)
			if (got.Preview != nil) != tc.valid || (got.Error == "") != tc.valid {
				t.Fatalf("preview=%+v", got)
			}
		})
	}
}

func TestOpenCurrentUsesServiceOriginAndRejectsStaleOrUnsafeTargets(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "original.png")
	if err := os.WriteFile(original, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, target, page, expected string
		imageID                      int64
		change                       string
	}{
		{"folder", "folder", "", root, 7, ""},
		{"page", "page", "https://wallhaven.cc/w/example", "https://wallhaven.cc/w/example", 7, ""},
		{"stale identity", "folder", "", "", 8, ""},
		{"changed during read", "folder", "", "", 7, "image"},
		{"service replaced during read", "folder", "", "", 7, "service"},
		{"missing page", "page", "", "", 7, ""},
		{"script page", "page", "javascript:alert(1)", "", 7, ""},
		{"file page", "page", "file:///etc/passwd", "", 7, ""},
		{"credential page", "page", "https://secret@example.com/image", "", 7, ""},
		{"arbitrary target", original, "", "", 7, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var reads atomic.Int32
			path := serveFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/status" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					return
				}
				id, instance := "accepted", "service-one"
				if reads.Add(1) > 1 {
					switch tc.change {
					case "image":
						id = "changed"
					case "service":
						instance = "service-two"
					}
				}
				_ = json.NewEncoder(w).Encode(control.Status{Version: 1, InstanceID: instance,
					Current:      &playback.Entry{ID: id, ImageID: 7, Path: "/cache/not-the-original.png"},
					CurrentImage: &playback.CurrentImage{OriginalPath: original, Attribution: &source.Attribution{PageURL: tc.page}},
				})
			}))
			opened := ""
			client := &Client{ctx: t.Context(), socket: path, opener: func(_ context.Context, location string) error { opened = location; return nil }}
			got := client.OpenCurrent(tc.imageID, tc.target)
			if opened != tc.expected || got.Opened != (tc.expected != "") || (got.Error == "") != got.Opened {
				t.Fatalf("open=%+v location=%q expected=%q", got, opened, tc.expected)
			}
		})
	}
	if err := os.Remove(original); err != nil {
		t.Fatal(err)
	}
	if _, err := openLocation(playback.HistoryEntry{OriginalPath: original}, "folder"); err == nil {
		t.Fatal("missing original accepted")
	}
}
