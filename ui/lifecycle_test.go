package main

import (
	"context"
	"image"
	"image/color"
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
	"github.com/vadim82/panora/internal/service"
	"github.com/vadim82/panora/internal/sources/local"
	"github.com/vadim82/panora/ui/internal/menubar"
)

type lifecycleDesktop struct{ calls atomic.Int32 }

func (*lifecycleDesktop) Probe(context.Context) error { return nil }
func (d *lifecycleDesktop) SetWallpaper(context.Context, string, desktop.DisplayOptions) error {
	d.calls.Add(1)
	return nil
}

// This starts the real service with separate ownership/cancellation and a fake
// setter. It exercises the exact bridge used by the native window, not a mock API.
func TestClientEnrollmentPreviewCloseReopenAndServiceReconnect(t *testing.T) {
	directory, err := os.MkdirTemp("/tmp", "pc-live-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	configPath := filepath.Join(directory, "config.toml")
	if err := os.WriteFile(configPath, []byte("version=1\n[playback]\ninterval='150ms'\n[filters]\nmin_width=1\nmin_height=1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	images := filepath.Join(directory, "Wallpapers")
	if err := os.Mkdir(images, 0700); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"one.png", "two.png"} {
		picture := image.NewRGBA(image.Rect(0, 0, 2, 2))
		picture.Set(0, 0, color.RGBA{R: uint8(i * 255), G: 128, A: 255})
		file, err := os.Create(filepath.Join(images, name))
		if err != nil {
			t.Fatal(err)
		}
		encodeErr := png.Encode(file, picture)
		closeErr := file.Close()
		if encodeErr != nil || closeErr != nil {
			t.Fatalf("write image=%v close=%v", encodeErr, closeErr)
		}
	}
	socket := filepath.Join(directory, "control.sock")
	desktopFixture := &lifecycleDesktop{}
	options := playback.ServiceOptions{Request: playback.Request{ConfigPath: configPath, StatePath: filepath.Join(directory, "state.db"), CachePath: filepath.Join(directory, "cache"), Source: local.Provider{}}, Paused: true, Backend: "fake"}
	resolve := func(context.Context) (desktop.WallpaperBackend, error) { return desktopFixture, nil }
	menuStops := make(chan struct{}, 1)
	menuReady := make(chan string, 1)
	hooks := service.Hooks{RunCompanion: func(ctx context.Context, instance string) {
		menu, err := menubar.Connect(ctx, socket, instance)
		if err != nil {
			t.Errorf("connect the companion: %v", err)
			return
		}
		defer menu.Close()
		menuReady <- instance
		menu.Run(ctx, nil, menuStops, func(menubar.State) {})
	}}
	start := func() func() {
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- service.Run(ctx, options, resolve, socket, io.Discard, io.Discard, hooks) }()
		stop := func() {
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("service stopped: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Error("service did not stop")
			}
		}
		awaitClientCondition(t, func() bool {
			_, err := control.Request(t.Context(), socket, "")
			return err == nil
		})
		return stop
	}
	stop := start()
	t.Cleanup(func() {
		if stop != nil {
			stop()
		}
	})
	ctx, closeWindow := context.WithCancel(t.Context())
	client := &Client{ctx: ctx, socket: socket, picker: func(context.Context) (string, error) { return images, nil }}
	initial := client.Status()
	if !initial.Connected || !initial.Status.SetupRequired || desktopFixture.calls.Load() != 0 {
		t.Fatalf("first run=%+v", initial)
	}
	if instance := waitMenu(t, menuReady); instance != initial.Status.InstanceID {
		t.Fatalf("first companion attached to %q", instance)
	}
	folder, err := client.PickFolder()
	if err != nil || folder.Name != "Wallpapers" {
		t.Fatalf("picker=%+v %v", folder, err)
	}
	config := client.Configuration()
	if config.Configuration == nil {
		t.Fatalf("config=%+v", config)
	}
	saved := client.AddFolderSettings(playback.AddFolderRequest{Revision: config.Configuration.Revision, Path: folder.Path})
	if saved.Configuration == nil || len(saved.Configuration.Folders) != 1 || saved.Configuration.Folders[0].Path != images {
		t.Fatalf("enrollment=%+v", saved)
	}
	awaitClientCondition(t, func() bool {
		status := client.Status()
		return status.Connected && !status.Status.SetupRequired && len(status.Status.Providers) == 1 && len(status.Status.Providers[0].Inputs) == 1 && status.Status.Providers[0].Inputs[0].State == "ready"
	})
	accepted := client.Action("next")
	if !accepted.Connected || accepted.Error != "" || accepted.Current == nil || accepted.Current.OriginalPath == accepted.Status.Current.Path || desktopFixture.calls.Load() != 1 {
		t.Fatalf("next=%+v calls=%d", accepted, desktopFixture.calls.Load())
	}
	var preview PreviewResult
	awaitClientCondition(t, func() bool {
		preview = client.Preview(accepted.Current.ImageID)
		return preview.Code != "busy"
	})
	if preview.Preview == nil || !preview.Preview.Available || preview.Preview.Width != 2 || desktopFixture.calls.Load() != 1 {
		t.Fatalf("preview=%+v calls=%d", preview, desktopFixture.calls.Load())
	}
	if got := client.Action("resume"); !got.Connected || got.Error != "" || got.Status.Paused {
		t.Fatalf("resume=%+v", got)
	}
	closeWindow()
	if got := client.Status(); got.Connected {
		t.Fatal("closed client retained live requests")
	}
	awaitClientCondition(t, func() bool { return desktopFixture.calls.Load() >= 2 })
	reopened := &Client{ctx: t.Context(), socket: socket}
	var state Snapshot
	awaitClientCondition(t, func() bool {
		state = reopened.Status()
		return state.Connected && state.Current != nil && state.Status.HistoryLength >= 2
	})
	if !state.Connected || state.Status.Paused || state.Current == nil || state.Status.HistoryLength < 2 {
		t.Fatalf("reopened=%+v", state)
	}
	if duplicate, err := playback.OpenService(t.Context(), options, resolve); err == nil {
		duplicate.Close()
		t.Fatal("a second service acquired playback ownership")
	}
	var paused Snapshot
	awaitClientCondition(t, func() bool {
		paused = reopened.Action("pause")
		return paused.Connected && paused.Error == "" && paused.Status.Paused
	})
	retainedID := paused.Status.Current.ID
	previousInstance := paused.Status.InstanceID
	menuStops <- struct{}{}
	awaitClientCondition(t, func() bool { return !reopened.Status().Connected })
	stop()
	stop = nil
	if got := reopened.Status(); got.Connected {
		t.Fatal("stopped service reported connected")
	}
	stop = start()
	if got := reopened.Status(); !got.Connected || got.Status.Current.ID != retainedID || !got.Status.Paused || got.Current == nil {
		t.Fatalf("service reconnect=%+v", got)
	}
	if instance := waitMenu(t, menuReady); instance == "" || instance == previousInstance {
		t.Fatalf("replacement companion attached to %q, previous instance was %q", instance, previousInstance)
	}
}

func waitMenu(t *testing.T, ready <-chan string) string {
	t.Helper()
	select {
	case instance := <-ready:
		return instance
	case <-time.After(5 * time.Second):
		t.Fatal("menu companion did not become ready")
		return ""
	}
}

func awaitClientCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("client/service condition did not become ready")
}
