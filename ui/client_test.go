package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/playback"
)

func serveFixture(t *testing.T, handler http.Handler) string {
	t.Helper()
	directory, err := os.MkdirTemp("/tmp", "pc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	path := filepath.Join(directory, "control.sock")
	listener, err := control.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return path
}

func writeStatus(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(control.Status{Version: control.APIVersion, Paused: true, Interval: "30m"})
}

func TestStatusAndDomainFailureReconcileWithoutRetry(t *testing.T) {
	var posts, reads atomic.Int32
	path := serveFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(map[string]any{"version": control.APIVersion, "error": control.Error{Code: "no_images", Message: "No images ready"}})
			return
		}
		reads.Add(1)
		writeStatus(w)
	}))
	client := &Client{ctx: context.Background(), socket: path}
	if got := client.Status(); !got.Connected || got.Status == nil || !got.Status.Paused {
		t.Fatalf("status = %+v", got)
	}
	got := client.Action("next")
	if !got.Connected || got.Status == nil || got.Code != "no_images" || got.Error != "No images ready" || posts.Load() != 1 || reads.Load() != 2 {
		t.Fatalf("action = %+v, posts=%d reads=%d", got, posts.Load(), reads.Load())
	}
}

func TestConcurrentActionAndCancellation(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var posts atomic.Int32
	path := serveFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		writeStatus(w)
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &Client{ctx: ctx, socket: path}
	done := make(chan Snapshot, 1)
	go func() { done <- client.Action("next") }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("action did not reach fixture")
	}
	if got := client.Action("next"); got.Code != "busy" || posts.Load() != 1 {
		t.Fatalf("concurrent action = %+v", got)
	}
	cancel()
	select {
	case got := <-done:
		if got.Connected || !strings.Contains(got.Error, "canceled") {
			t.Fatalf("canceled action = %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("action outlived client cancellation")
	}
	close(release)
}

func TestPickerDerivesNameAndHandlesCancel(t *testing.T) {
	for _, tc := range []struct {
		path, name string
		canceled   bool
		failure    bool
	}{
		{"/home/example/Pictures/Wallpapers/", "Wallpapers", false, false},
		{"", "", true, false},
		{"relative/path", "", false, true},
	} {
		client := &Client{ctx: context.Background(), picker: func(context.Context) (string, error) { return tc.path, nil }}
		got, err := client.PickFolder()
		if (err != nil) != tc.failure || got.Name != tc.name || got.Canceled != tc.canceled {
			t.Fatalf("picker(%q) = %+v, %v", tc.path, got, err)
		}
	}
	client := &Client{ctx: context.Background(), picker: func(context.Context) (string, error) { return "", errors.New("picker unavailable") }}
	if _, err := client.PickFolder(); err == nil || !strings.Contains(err.Error(), "picker unavailable") {
		t.Fatalf("picker error = %v", err)
	}
}

func TestFolderSaveReconcilesLostResponseWithoutReplay(t *testing.T) {
	var posts atomic.Int32
	path := serveFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/configuration/folders" && r.Method == http.MethodPost:
			posts.Add(1)
			var edit playback.AddFolderRequest
			if err := json.NewDecoder(r.Body).Decode(&edit); err != nil || edit.Revision != "reviewed" || edit.Path != "/Pictures/Wallpapers" {
				t.Errorf("edit=%+v %v", edit, err)
			}
			connection, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = connection.Close()
			}
		case r.URL.Path == "/v1/configuration" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(playback.Configuration{Version: 1, Revision: "reviewed"})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	client := &Client{ctx: t.Context(), socket: path}
	if got := client.Configuration(); got.Configuration == nil || got.Configuration.Revision != "reviewed" {
		t.Fatalf("read=%+v", got)
	}
	edit := playback.AddFolderRequest{Revision: "reviewed", Path: "/Pictures/Wallpapers"}
	if got := client.AddFolderSettings(edit); got.Configuration != nil || got.Code != "request_failed" || !strings.Contains(got.Error, "may have completed") || posts.Load() != 1 {
		t.Fatalf("lost save=%+v posts=%d", got, posts.Load())
	}
	if got := client.Configuration(); got.Configuration == nil || posts.Load() != 1 {
		t.Fatalf("reconcile=%+v posts=%d", got, posts.Load())
	}
	client.action.Lock()
	defer client.action.Unlock()
	if got := client.AddFolderSettings(edit); got.Code != "busy" || posts.Load() != 1 {
		t.Fatalf("concurrent save=%+v posts=%d", got, posts.Load())
	}
}

func TestSettingsPatchReturnsTypedErrorsAndNeverReplaysLostResponse(t *testing.T) {
	var patches atomic.Int32
	var mode atomic.Int32
	path := serveFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" || r.URL.Path != "/v1/configuration" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		patches.Add(1)
		var edit struct {
			Revision string
			MinWidth int `json:"min_width"`
		}
		if err := json.NewDecoder(r.Body).Decode(&edit); err != nil || edit.Revision != "reviewed" || edit.MinWidth != 1920 {
			t.Errorf("patch=%+v %v", edit, err)
		}
		switch mode.Load() {
		case 1:
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"version": 1, "error": control.Error{Code: "config_conflict", Message: "reload settings"}})
		case 2:
			connection, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = connection.Close()
			}
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"version": 1, "revision": "saved", "saved": map[string]any{"interval": "2m"}, "effective": map[string]any{"interval": "30m0s"}, "restart_required": []string{"interval"}})
		}
	}))
	client := &Client{ctx: context.Background(), socket: path}
	width := 1920
	edit := playback.UpdateConfigurationRequest{Revision: "reviewed", MinWidth: &width}
	if got := client.UpdateConfiguration(edit); got.Configuration == nil || got.Configuration.Revision != "saved" || len(got.Configuration.RestartRequired) != 1 {
		t.Fatalf("patch=%+v", got)
	}
	mode.Store(1)
	if got := client.UpdateConfiguration(edit); got.Code != "config_conflict" || got.Configuration != nil {
		t.Fatalf("conflict=%+v", got)
	}
	mode.Store(2)
	if got := client.UpdateConfiguration(edit); got.Code != "request_failed" || !strings.Contains(got.Error, "reload settings before retrying") || patches.Load() != 3 {
		t.Fatalf("lost patch=%+v count=%d", got, patches.Load())
	}
	client.action.Lock()
	if got := client.UpdateConfiguration(edit); got.Code != "busy" || patches.Load() != 3 {
		t.Fatalf("concurrent patch=%+v", got)
	}
	client.action.Unlock()
}
