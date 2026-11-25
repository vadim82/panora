package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/sources/local"
	"image"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/vadim82/panora/internal/playback"
)

func TestRulePreviewBridgeKeepsDraftReadOnlyAndPartialCounts(t *testing.T) {
	var received playback.RulePreviewRequest
	socket := serveFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/rules/preview" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(`{"version":1,"revision":"reviewed","scope":"enabled_catalog","limit":200,"inspected":9,"matched":3,"rejected":2,"unknown":4,"complete":false,"reasons":{"unknown_remote":4}}`))
	}))
	request := playback.RulePreviewRequest{Revision: "reviewed", Rules: playback.ImageRules{MinWidth: 1920, MinHeight: 1080, Ratio: "presets", RatioPresets: []string{"21:9"}}}
	result := (&Client{ctx: t.Context(), socket: socket}).RulePreview(request)
	if received.Revision != "reviewed" || received.Rules.Ratio != "presets" || len(received.Rules.RatioPresets) != 1 || result.Preview == nil || result.Preview.Complete || result.Preview.Unknown != 4 || result.Preview.Inspected != 9 {
		t.Fatalf("request=%+v result=%+v", received, result)
	}
}

func TestDraftPreviewBridgeAgainstLiveIsolatedService(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "pc-rules-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	images := filepath.Join(root, "images")
	if err := os.Mkdir(images, 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(images, "image.png"))
	if err != nil {
		t.Fatal(err)
	}
	encodeErr := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	closeErr := file.Close()
	if encodeErr != nil || closeErr != nil {
		t.Fatalf("image=%v close=%v", encodeErr, closeErr)
	}
	configPath := filepath.Join(root, "config.toml")
	original := []byte(fmt.Sprintf("version=1\n[filters]\nmin_width=1\nmin_height=1\n[[providers]]\nid='local'\nenabled=true\n[[providers.folders]]\nid='test'\npath=%q\n", images))
	if err := os.WriteFile(configPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	backend := &lifecycleDesktop{}
	domain, err := playback.OpenService(ctx, playback.ServiceOptions{Request: playback.Request{ConfigPath: configPath, StatePath: filepath.Join(root, "state.db"), CachePath: filepath.Join(root, "cache"), Source: local.Provider{}}, Paused: true, Backend: "fake"}, func(context.Context) (desktop.WallpaperBackend, error) { return backend, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer domain.Close()
	domain.StartDiscovery()
	listener, err := control.Listen(filepath.Join(root, "control.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{Handler: control.Handler(domain)}
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	defer func() { _ = server.Close(); <-done }()
	client := &Client{ctx: ctx, socket: filepath.Join(root, "control.sock")}
	awaitClientCondition(t, func() bool {
		status := domain.Status()
		return len(status.Providers) == 1 && len(status.Providers[0].Inputs) == 1 && status.Providers[0].Inputs[0].State == "ready"
	})
	config := client.Configuration()
	if config.Configuration == nil {
		t.Fatalf("config=%+v", config)
	}
	result := client.RulePreview(playback.RulePreviewRequest{Revision: config.Configuration.Revision, Rules: config.Configuration.Rules})
	if result.Preview == nil || result.Error != "" || result.Preview.Inspected != 1 || result.Preview.Matched != 1 || result.Preview.Unknown != 0 || result.Preview.ExclusionsApplied || result.Preview.Validation != "local_headers_and_validated_cache" {
		t.Fatalf("preview=%+v", result)
	}
	saved, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(saved, original) {
		t.Fatalf("preview changed saved rules: %v", err)
	}
	status := domain.Status()
	if backend.calls.Load() != 0 || status.Current != nil || status.HistoryLength != 0 || !status.Paused {
		t.Fatalf("preview changed playback: %+v calls=%d", status, backend.calls.Load())
	}
}
