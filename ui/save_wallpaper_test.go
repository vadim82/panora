package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/vadim82/panora/internal/playback"
)

func TestSaveWallpaperReportsActualDestination(t *testing.T) {
	actual := t.TempDir()
	edit := playback.SaveWallpaperRequest{Instance: "service", History: "accepted", ImageID: 41, Revision: "reviewed"}
	socket := serveFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/wallpapers/save":
			var received playback.SaveWallpaperRequest
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil || received != edit || r.Method != http.MethodPost {
				t.Errorf("save request=%+v, method=%s, error=%v", received, r.Method, err)
			}
			_ = json.NewEncoder(w).Encode(playback.SavedWallpaper{Version: 1, Path: filepath.Join(actual, "image.png")})
		default:
			t.Errorf("unexpected request to %s", r.URL.Path)
		}
	}))
	client := &Client{ctx: t.Context(), socket: socket}
	if save := client.SaveWallpaper(edit); !save.Saved || save.FolderName != filepath.Base(actual) {
		t.Fatalf("save=%+v", save)
	}
}
