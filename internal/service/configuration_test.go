package service

import (
	"context"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/playback"
	"github.com/vadim82/panora/internal/sources/local"
)

type enrollmentBackend struct{ calls atomic.Int32 }

func (*enrollmentBackend) Probe(context.Context) error { return nil }
func (b *enrollmentBackend) SetWallpaper(context.Context, string, desktop.DisplayOptions) error {
	b.calls.Add(1)
	return nil
}

func TestRunningServiceEnrollsThroughPrivateSocket(t *testing.T) {
	directory, err := os.MkdirTemp("", "panora-enroll-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	path := filepath.Join(directory, "config.toml")
	if err := os.WriteFile(path, []byte("version=1\n[filters]\nmin_width=2\nmin_height=2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(directory, "Wallpapers")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	imageFile, err := os.Create(filepath.Join(root, "wallpaper.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(imageFile, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if err := imageFile.Close(); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(directory, "control.sock")
	backend := &enrollmentBackend{}
	options := playback.ServiceOptions{Request: playback.Request{ConfigPath: path, StatePath: filepath.Join(directory, "state.db"), CachePath: filepath.Join(directory, "cache"), Source: local.Provider{}, Options: desktop.DisplayOptions{Fit: "auto"}}, Interval: time.Hour, IntervalChanged: true, Paused: true, Backend: "fake"}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, options, func(context.Context) (desktop.WallpaperBackend, error) { return backend, nil }, socket, io.Discard, io.Discard, Hooks{})
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
	deadline := time.Now().Add(5 * time.Second)
	var initial playback.Configuration
	for {
		initial, err = control.ReadConfiguration(t.Context(), socket)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if initial.Effective.Interval != "1h0m0s" || len(initial.Folders) != 0 {
		t.Fatalf("initial=%+v", initial)
	}
	inherit := true
	saved, err := control.AddFolder(t.Context(), socket, playback.AddFolderRequest{Revision: initial.Revision, Path: root, InheritRules: &inherit})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Folders) != 1 || saved.Folders[0].Name != "Wallpapers" {
		t.Fatalf("saved=%+v", saved)
	}
	for {
		status, err := control.Request(t.Context(), socket, "")
		if err != nil {
			t.Fatal(err)
		}
		if !status.SetupRequired && len(status.Providers) == 1 && status.Providers[0].Inputs[0].State == "ready" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("enrollment did not finish: %+v", status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if backend.calls.Load() != 0 {
		t.Fatal("enrollment cleared pause")
	}
	status, err := control.Request(t.Context(), socket, "next")
	if err != nil || status.Current == nil || !status.Paused || backend.calls.Load() != 1 {
		t.Fatalf("next=%+v %v calls=%d", status, err, backend.calls.Load())
	}
	preview, err := control.ReadPreview(t.Context(), socket, status.Current.ImageID)
	if err != nil || !preview.Available || preview.MIMEType != "image/png" || preview.Width != 2 || preview.Height != 2 || len(preview.Data) == 0 || backend.calls.Load() != 1 {
		t.Fatalf("preview=%+v %v calls=%d", preview, err, backend.calls.Load())
	}
	_, err = control.AddFolder(t.Context(), socket, playback.AddFolderRequest{Revision: initial.Revision, Path: directory})
	if typed, ok := err.(*control.Error); !ok || typed.Code != "config_conflict" {
		t.Fatalf("stale save=%v", err)
	}
	reread, err := control.ReadConfiguration(t.Context(), socket)
	if err != nil || reread.Revision != saved.Revision || reread.Folders[0].ID != saved.Folders[0].ID {
		t.Fatalf("reread=%+v %v", reread, err)
	}
	width, interval := 3, "2m"
	updated, err := control.UpdateConfiguration(t.Context(), socket, playback.UpdateConfigurationRequest{Revision: reread.Revision, MinWidth: &width, Interval: &interval})
	if err != nil || updated.Saved.Interval != "2m" || updated.Effective.Interval != "2m0s" || updated.Rules.MinWidth != 3 {
		t.Fatalf("update=%+v %v", updated, err)
	}
	if _, err := control.Request(t.Context(), socket, "next"); err == nil || backend.calls.Load() != 1 {
		t.Fatalf("new image rules not applied: %v calls=%d", err, backend.calls.Load())
	}
	width = 2
	restored, err := control.UpdateConfiguration(t.Context(), socket, playback.UpdateConfigurationRequest{Revision: updated.Revision, MinWidth: &width})
	if err != nil {
		t.Fatal(err)
	}
	if status, err := control.Request(t.Context(), socket, "next"); err != nil || status.Current == nil || backend.calls.Load() != 2 {
		t.Fatalf("restored rules=%+v %v", status, err)
	}
	_, err = control.UpdateConfiguration(t.Context(), socket, playback.UpdateConfigurationRequest{Revision: restored.Revision})
	if typed, ok := err.(*control.Error); !ok || typed.Code != "invalid_request" {
		t.Fatalf("empty preference edit=%v", err)
	}

	// The same socket and revision writer manage an existing folder at runtime.
	folderID := restored.Folders[0].ID
	disabled := false
	editedFolder, err := control.UpdateFolder(t.Context(), socket, playback.UpdateFolderRequest{Revision: restored.Revision, ID: folderID, Enabled: &disabled})
	if err != nil || editedFolder.Folders[0].Enabled || editedFolder.Folders[0].InputEnabled {
		t.Fatalf("disable=%+v %v", editedFolder, err)
	}
	if _, err := control.Request(t.Context(), socket, "next"); err == nil || backend.calls.Load() != 2 {
		t.Fatalf("disabled input selected=%v", err)
	}
	enabled, label, depth := true, "Desktop images", 0
	editedFolder, err = control.UpdateFolder(t.Context(), socket, playback.UpdateFolderRequest{Revision: editedFolder.Revision, ID: folderID, Name: &label, Enabled: &enabled, MaxDepth: &depth})
	if err != nil || editedFolder.Folders[0].ID != folderID || editedFolder.Folders[0].Name != label || !editedFolder.Folders[0].Enabled || editedFolder.Folders[0].EffectiveMaxDepth != 0 {
		t.Fatalf("edit=%+v %v", editedFolder, err)
	}
	removed, err := control.RemoveFolder(t.Context(), socket, playback.RemoveFolderRequest{Revision: editedFolder.Revision, ID: folderID})
	if err != nil || len(removed.Folders) != 0 {
		t.Fatalf("remove=%+v %v", removed, err)
	}
	if _, err := control.RemoveFolder(t.Context(), socket, playback.RemoveFolderRequest{Revision: removed.Revision, ID: folderID}); err == nil {
		t.Fatal("missing folder removal succeeded")
	}
	status, err = control.Request(t.Context(), socket, "")
	if err != nil || status.HistoryLength != 2 || backend.calls.Load() != 2 {
		t.Fatalf("removal changed history or desktop=%+v %v", status, err)
	}
	if _, err := os.Stat(filepath.Join(root, "wallpaper.png")); err != nil {
		t.Fatal(err)
	}
}
