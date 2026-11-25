package service

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/playback"
	"github.com/vadim82/panora/internal/sources/local"
)

func TestSearchManagementThroughPrivateSocket(t *testing.T) {
	directory, err := os.MkdirTemp("", "panora-search-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	path := filepath.Join(directory, "config.toml")
	if err := os.WriteFile(path, []byte("version=1\n"), 0600); err != nil {
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
	var before playback.Configuration
	for {
		before, err = control.ReadConfiguration(t.Context(), socket)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	saved, err := control.AddSearch(t.Context(), socket, playback.AddSearchRequest{Revision: before.Revision, Query: "nature"})
	if err != nil || len(saved.Searches) != 1 || saved.Searches[0].Name != "nature" || saved.Searches[0].Purity != "sfw" {
		t.Fatalf("add=%+v %v", saved, err)
	}
	name, query, enabled, categories, sorting, order, pages, topRange, interval := "Edited", "stars", false, "110", "toplist", "asc", 2, "1w", "15m"
	saved, err = control.UpdateSearch(t.Context(), socket, playback.UpdateSearchRequest{Revision: saved.Revision, ID: saved.Searches[0].ID, Name: &name, Query: &query, Enabled: &enabled, Categories: &categories, Sorting: &sorting, Order: &order, MaxPages: &pages, TopRange: &topRange, RefreshInterval: &interval})
	if err != nil || saved.Searches[0].Enabled || saved.Searches[0].Query != "stars" || saved.Searches[0].Categories != "110" || saved.Searches[0].MaxPages != 2 {
		t.Fatalf("edit=%+v %v", saved, err)
	}
	reread, err := control.ReadConfiguration(t.Context(), socket)
	if err != nil || reread.Revision != saved.Revision || reread.Searches[0].ID != saved.Searches[0].ID {
		t.Fatalf("read=%+v %v", reread, err)
	}
	removed, err := control.RemoveSearch(t.Context(), socket, playback.RemoveSearchRequest{Revision: saved.Revision, ID: saved.Searches[0].ID})
	if err != nil || len(removed.Searches) != 0 {
		t.Fatalf("remove=%+v %v", removed, err)
	}
	if _, err := control.RemoveSearch(t.Context(), socket, playback.RemoveSearchRequest{Revision: removed.Revision, ID: saved.Searches[0].ID}); err == nil {
		t.Fatal("missing search removal succeeded")
	}
	if backend.calls.Load() != 0 {
		t.Fatal("saving search settings changed the desktop")
	}
}
