package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/vadim82/panora/internal/cache"
	"github.com/vadim82/panora/internal/playback"
)

func TestOpenConfigurationUsesConnectedServiceFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "custom config.toml")
	if err := os.WriteFile(configPath, []byte("# fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	socket := serveFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/configuration" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(playback.Configuration{Version: 1, ConfigPath: configPath})
	}))
	var opened string
	client := &Client{ctx: t.Context(), socket: socket, configOpener: func(_ context.Context, path string) error {
		opened = path
		return nil
	}}
	if result := client.OpenConfiguration(); !result.Opened || opened != configPath {
		t.Fatalf("open result = %+v, path = %q", result, opened)
	}
}

func TestCacheBridgePreservesProtectedAndUnknownUsage(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "protected", true: "unknown"}[unknown], func(t *testing.T) {
			path := serveFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/v1/cache" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				result := playback.CacheStatus{Version: 1, Usage: cache.Usage{State: "protected_over_budget", Images: 3, Bytes: 300, ProtectedImages: 2, ProtectedBytes: 200}}
				if unknown {
					result.Usage = cache.Usage{State: "unknown"}
					result.UsageError = &playback.Error{Code: "cache_usage_failed", Message: "Unavailable"}
				}
				_ = json.NewEncoder(w).Encode(result)
			}))
			result := (&Client{ctx: t.Context(), socket: path}).CacheStatus()
			if result.Cache == nil || result.Error != "" || (unknown && result.Cache.UsageError == nil) || (!unknown && result.Cache.Usage.ProtectedBytes != 200) {
				t.Fatalf("cache=%+v", result)
			}
		})
	}
}
