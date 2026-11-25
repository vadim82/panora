package main

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/vadim82/panora/internal/playback"
)

func TestFolderBridgeUsesReviewedOperations(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			var body map[string]any
			socket := serveFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != method || r.URL.Path != "/v1/configuration/folders" {
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				_ = json.NewEncoder(w).Encode(playback.Configuration{Version: 1, Revision: "saved", Folders: []playback.ConfiguredFolder{{ID: "stable", Path: "/Pictures", Name: "Pictures"}}})
			}))
			client := &Client{ctx: t.Context(), socket: socket}
			var result SettingsResult
			switch method {
			case http.MethodPost:
				result = client.AddFolderSettings(playback.AddFolderRequest{Revision: "reviewed", Path: "/Pictures"})
			case http.MethodPatch:
				enabled := false
				result = client.UpdateFolder(playback.UpdateFolderRequest{Revision: "reviewed", ID: "stable", Enabled: &enabled})
			case http.MethodDelete:
				result = client.RemoveFolder(playback.RemoveFolderRequest{Revision: "reviewed", ID: "stable"})
			}
			if body["revision"] != "reviewed" || result.Error != "" || result.Configuration == nil || result.Configuration.Revision != "saved" || len(result.Configuration.Folders) != 1 || result.Configuration.Folders[0].ID != "stable" {
				t.Fatalf("request=%v result=%+v", body, result)
			}
			if method == http.MethodPost && body["path"] != "/Pictures" {
				t.Fatalf("path=%v", body)
			}
			if method != http.MethodPost && body["id"] != "stable" {
				t.Fatalf("identity=%v", body)
			}
			if method == http.MethodPatch && body["enabled"] != false {
				t.Fatalf("disabled flag=%v", body)
			}
		})
	}
}
