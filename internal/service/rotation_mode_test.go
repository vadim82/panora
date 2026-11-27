package service

import (
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/cache"
	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/playback"
	"github.com/vadim82/panora/internal/source"
	"github.com/vadim82/panora/internal/sources/local"
)

func TestRotationModeThroughPrivateSocket(t *testing.T) {
	directory, err := os.MkdirTemp("", "panora-shuffle-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	remoteFixture := filepath.Join(directory, "remote.png")
	file, err := os.Create(remoteFixture)
	if err != nil {
		t.Fatal(err)
	}
	encodeErr := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 2, 3)))
	if closeErr := file.Close(); encodeErr != nil || closeErr != nil {
		t.Fatalf("write fixture: %v %v", encodeErr, closeErr)
	}
	path := filepath.Join(directory, "config.toml")
	body := `version=1
[[providers]]
id="wallhaven"
enabled=true
[[providers.searches]]
id="remote"
query="fixture"
`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	config, _, err := playback.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	// Reopen a catalog with one accepted, cached Wallhaven wallpaper. The
	// provider stays offline and no desktop or network resource is contacted.
	ctx := t.Context()
	statePath := filepath.Join(directory, "state.db")
	cachePath := filepath.Join(directory, "cache")
	seeded := seedRemoteRotationFixture(t, statePath, cachePath, remoteFixture, config.InputRevision("wallhaven", "remote"))
	socket := filepath.Join(directory, "control.sock")
	backend := &enrollmentBackend{}
	resolve := func(context.Context) (desktop.WallpaperBackend, error) { return backend, nil }
	options := playback.ServiceOptions{Request: playback.Request{ConfigPath: path, StatePath: statePath, CachePath: cachePath, Source: local.Provider{}, Options: desktop.DisplayOptions{Fit: "auto"}}, Paused: true, Backend: "fake"}
	runCtx, cancel := context.WithCancel(ctx)
	ready, done := make(chan struct{}), make(chan error, 1)
	go func() {
		done <- Run(runCtx, options, resolve, socket, io.Discard, io.Discard, Hooks{Ready: func(context.Context) error {
			close(ready)
			return nil
		}})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("shutdown=%v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("service did not stop")
		}
	})
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("service did not start")
	}

	initial, err := control.Request(ctx, socket, "")
	if err != nil || initial.Current == nil || initial.Current.ImageID != seeded || !initial.Paused {
		t.Fatalf("could not reopen the accepted wallpaper: %+v %v", initial, err)
	}
	settings, err := control.ReadConfiguration(ctx, socket)
	if err != nil || settings.Saved.RotationMode != "balance_sources" {
		t.Fatalf("initial settings=%+v %v", settings, err)
	}
	mode := "shuffle_all_images"
	saved, err := control.UpdateConfiguration(ctx, socket, playback.UpdateConfigurationRequest{Revision: settings.Revision, RotationMode: &mode})
	if err != nil || saved.Saved.RotationMode != mode || saved.Effective.RotationMode != mode {
		t.Fatalf("save mode=%+v %v", saved, err)
	}
	after, err := control.Request(ctx, socket, "")
	if err != nil || !reflect.DeepEqual(after.Current, initial.Current) || after.HistoryCursor != initial.HistoryCursor || backend.calls.Load() != 0 {
		t.Fatalf("save changed accepted state: before=%+v after=%+v %v", initial, after, err)
	}
	reread, err := control.ReadConfiguration(ctx, socket)
	if err != nil || reread.Revision != saved.Revision || reread.Saved.RotationMode != mode || reread.Effective.RotationMode != mode {
		t.Fatalf("reread mode=%+v %v", reread, err)
	}
}

func seedRemoteRotationFixture(t *testing.T, statePath, cachePath, imagePath, revision string) int64 {
	t.Helper()
	ctx := t.Context()
	imageCache, err := cache.New(cache.Options{Dir: cachePath})
	if err != nil {
		t.Fatal(err)
	}
	defer imageCache.Close()
	staged, err := imageCache.Materialize(ctx, source.Content{Kind: "local", MediaKind: "image", Path: imagePath})
	if err != nil {
		t.Fatal(err)
	}
	store, err := catalog.Open(ctx, statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	wallpaper := source.Candidate{ID: "remote-fixture", MediaKind: "image", Ref: json.RawMessage(`{"id":"remote-fixture"}`),
		Content: &source.Content{Kind: "https", MediaKind: "image", URL: "https://example.com/wallpaper.png"}}
	id, err := store.EnsureImage(ctx, "wallhaven", "remote", revision, "", wallpaper)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCachedImage(ctx, id.ImageID, catalog.CachedFile{
		Hash: staged.Hash, Path: staged.Path, Width: staged.Width, Height: staged.Height, Bytes: staged.Bytes,
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	err = store.SaveState(ctx, catalog.State{Paused: true, LastActionAt: now,
		History: []catalog.Record{{Reference: "accepted-fixture", Identity: id, Path: staged.Path, AcceptedAt: now}}})
	if err != nil {
		t.Fatal(err)
	}
	return id.ImageID
}
