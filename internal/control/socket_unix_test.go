//go:build linux || darwin

package control

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/playback"
	"github.com/vadim82/panora/internal/source"
)

func socketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "panora-sock-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "control.sock")
}

func serveFixture(t *testing.T, path string, handler http.Handler) {
	t.Helper()
	listener, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Close()
		<-done
		listener.Close()
	})
}

func TestCurrentExclusionSocketClient(t *testing.T) {
	path := socketPath(t)
	serveFixture(t, path, Handler(&exclusionsFixture{}))
	status, err := ExcludeCurrent(t.Context(), path, "current", playback.HistoryReference{ID: "accepted-1", ImageID: 7})
	if err != nil || !status.CurrentExcluded {
		t.Fatalf("matching request failed: %+v, %v", status, err)
	}
}

func TestPrivateSocketRoundTripAndOwnership(t *testing.T) {
	path := socketPath(t)
	serveFixture(t, path, Handler(&testService{}))
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("socket not private: %v %v", info, err)
	}
	status, err := Request(context.Background(), path, "")
	if err != nil || status.Version != APIVersion || !status.Paused {
		t.Fatalf("status failed: %+v %v", status, err)
	}
	if _, err := Request(context.Background(), path, "next"); err != nil {
		t.Fatal(err)
	}
	_, err = Request(context.Background(), path, "previous")
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Code != "no_previous" {
		t.Fatalf("typed error lost: %v", err)
	}
	if other, err := Listen(path); err == nil {
		other.Close()
		t.Fatal("replaced a live service socket")
	}
	if _, err := Request(context.Background(), path, ""); err != nil {
		t.Fatalf("duplicate service damaged existing socket: %v", err)
	}
}

func TestStaleSocketRecoveryAndNonSocketProtection(t *testing.T) {
	path := socketPath(t)
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	stale.Close()
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"", "next", "previous", "pause", "resume"} {
		_, err := Request(context.Background(), path, action)
		if err == nil || !strings.Contains(err.Error(), "start panora serve") || strings.Contains(err.Error(), "may have completed") {
			t.Fatalf("stale socket %q should explain service startup: %v", action, err)
		}
	}
	listener, err := Listen(path)
	if err != nil {
		t.Fatalf("could not reclaim stale socket: %v", err)
	}
	listener.Close()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket survived close: %v", err)
	}
	if err := os.WriteFile(path, []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}
	if listener, err := Listen(path); err == nil {
		listener.Close()
		t.Fatal("replaced an ordinary file")
	}
	if body, _ := os.ReadFile(path); string(body) != "keep me" {
		t.Fatal("ordinary file was modified")
	}
}

func TestSocketRejectsUnsafeDirectoriesAndPaths(t *testing.T) {
	path := socketPath(t)
	if err := os.Chmod(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if listener, err := Listen(path); err == nil {
		listener.Close()
		t.Fatal("accepted non-private directory")
	}
	if runtime.GOOS == "linux" {
		t.Setenv("XDG_RUNTIME_DIR", "")
		if _, err := SocketPath(""); err == nil {
			t.Fatal("silently fell back from absent runtime directory")
		}
		t.Setenv("XDG_RUNTIME_DIR", filepath.Dir(path))
		if _, err := SocketPath(""); err == nil {
			t.Fatal("accepted unsafe runtime directory")
		}
	}
	if _, err := SocketPath("relative.sock"); err == nil {
		t.Fatal("accepted relative socket path")
	}
	private := filepath.Dir(socketPath(t))
	symlink := filepath.Join(filepath.Dir(path), "linked")
	if err := os.Symlink(private, symlink); err != nil {
		t.Fatal(err)
	}
	if listener, err := Listen(filepath.Join(symlink, "control.sock")); err == nil {
		listener.Close()
		t.Fatal("accepted symlink socket directory")
	}
}

func TestClientDoesNotReplayUncertainMutation(t *testing.T) {
	path := socketPath(t)
	var calls atomic.Int32
	serveFixture(t, path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		connection, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			connection.Close() // Simulate a lost response after accepting the action.
		}
	}))
	_, err := Request(context.Background(), path, "next")
	if err == nil || !strings.Contains(err.Error(), "may have completed") || calls.Load() != 1 {
		t.Fatalf("uncertain request replayed or unexplained: calls=%d err=%v", calls.Load(), err)
	}
}

func TestClientRejectsIncompatibleAPI(t *testing.T) {
	path := socketPath(t)
	serveFixture(t, path, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":99,"paused":true}`))
	}))
	if _, err := Request(context.Background(), path, ""); err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("accepted incompatible API: %v", err)
	}
}

func TestExclusionSocketClientRoundTrip(t *testing.T) {
	path := socketPath(t)
	serveFixture(t, path, Handler(&exclusionsFixture{}))
	page, err := ListExclusions(t.Context(), path, 15, 2)
	if err != nil || page.Version != 1 || len(page.Entries) != 1 || page.Entries[0].ID != 12 || page.NextBefore != 12 {
		t.Fatalf("list: %+v %v", page, err)
	}
	if _, err := RestoreExclusion(t.Context(), path, 12); err != nil {
		t.Fatal(err)
	}
	_, err = RestoreExclusion(t.Context(), path, 99)
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != "exclusion_not_found" {
		t.Fatalf("restore error: %v", err)
	}
	if _, err := Request(t.Context(), path, "exclude"); err != nil {
		t.Fatal(err)
	}
	if _, err := Request(t.Context(), path, "undo-exclude"); err != nil {
		t.Fatal(err)
	}
}

func TestConfigurationClientAllowsEscapedSavedTOMLAndFolderSummaries(t *testing.T) {
	path := socketPath(t)
	root := "/" + strings.Repeat("&", 1<<19)
	body := "version=1\n[[providers]]\nid='local'\n[[providers.folders]]\nid='folder'\npath='" + root + "'\n"
	config := playback.Configuration{Version: APIVersion, Revision: "reviewed", TOML: body, Folders: []playback.ConfiguredFolder{{ID: "folder", Name: filepath.Base(root), Path: root}}}
	encoded, err := json.Marshal(config)
	if err != nil || len(encoded) <= 6*(1<<20) || len(encoded) > playback.MaxConfigurationMessageBytes {
		t.Fatalf("encoded bytes=%d %v", len(encoded), err)
	}
	serveFixture(t, path, Handler(&configurationFixture{result: &config}))
	got, err := ReadConfiguration(t.Context(), path)
	if err != nil || got.TOML != body || len(got.Folders) != 1 || got.Folders[0].Path != root {
		t.Fatalf("large configuration response failed: %v", err)
	}
}

func TestHistoryClientAllowsMaximumEscapedMetadataPage(t *testing.T) {
	path := socketPath(t)
	escaped := strings.Repeat("<", 4096)
	page := playback.HistoryPage{Version: APIVersion, Entries: []playback.HistoryEntry{}}
	for range playback.MaxHistoryPageSize {
		page.Entries = append(page.Entries, playback.HistoryEntry{ID: strings.Repeat("a", 64), SourceID: strings.Repeat("b", 128), Image: escaped, OriginalPath: escaped, Attribution: &source.Attribution{Title: escaped, Author: escaped, PageURL: escaped, SourceURL: escaped, AuthorURL: escaped, LicenseURL: escaped}})
	}
	encoded, err := json.Marshal(page)
	if err != nil || len(encoded) <= 1<<20 || len(encoded) > playback.MaxHistoryMessageBytes {
		t.Fatalf("response bytes=%d %v", len(encoded), err)
	}
	serveFixture(t, path, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(page) }))
	got, err := ListHistory(t.Context(), path, "", playback.MaxHistoryPageSize)
	if err != nil || len(got.Entries) != playback.MaxHistoryPageSize || got.Entries[0].Attribution.Title != escaped {
		t.Fatalf("large history page entries=%d %v", len(got.Entries), err)
	}
}
