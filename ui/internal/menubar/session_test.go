//go:build darwin || linux

package menubar

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/filelock"
)

func testSocket(t *testing.T, handle http.HandlerFunc) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "panora-menu-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "control.sock")
	listener, err := control.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handle, ReadHeaderTimeout: time.Second}
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	closeServer := func() { _ = server.Close(); _ = listener.Close(); <-done }
	t.Cleanup(closeServer)
	return path, closeServer
}

func await(t *testing.T, done <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not finish", name)
	}
}

func TestStopAcknowledgedWhilePlaybackIsBlocked(t *testing.T) {
	playbackStarted, playbackEnded := make(chan struct{}), make(chan struct{})
	stopRequested, ack := make(chan struct{}), make(chan struct{})
	var stopCalls atomic.Int32
	path, _ := testSocket(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/status" {
			_ = json.NewEncoder(w).Encode(control.Status{Version: 1, InstanceID: "observed", HistoryCursor: 2})
			return
		}
		if r.Method != http.MethodPost || r.URL.Query().Get("instance") != "observed" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			return
		}
		switch r.URL.Path {
		case "/v1/actions/next/for-instance":
			close(playbackStarted)
			<-r.Context().Done()
			close(playbackEnded)
		case "/v1/shutdown":
			stopCalls.Add(1)
			close(stopRequested)
			select {
			case <-ack:
				_ = json.NewEncoder(w).Encode(control.ShutdownResult{Version: 1, InstanceID: "observed", Stopping: true})
			case <-r.Context().Done():
			}
		default:
			t.Errorf("unexpected route %s", r.URL)
		}
	})
	session, err := Connect(t.Context(), path, "observed")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	actions := make(chan Action, 1)
	stops := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() { defer close(done); session.Run(t.Context(), actions, stops, func(State) {}) }()
	actions <- Next
	await(t, playbackStarted, "playback dispatch")
	stops <- struct{}{}
	await(t, stopRequested, "stop dispatch during playback")
	select {
	case <-done:
		t.Fatal("menu exited before the acknowledgement")
	default:
	}
	close(ack)
	await(t, done, "menu exit")
	await(t, playbackEnded, "pending playback cancellation")
	if stopCalls.Load() != 1 {
		t.Fatalf("shutdown sent %d times", stopCalls.Load())
	}
}

func TestMenuSingletonAndExternalShutdown(t *testing.T) {
	var mutations atomic.Int32
	path, closeServer := testSocket(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/status" {
			mutations.Add(1)
		}
		_ = json.NewEncoder(w).Encode(control.Status{Version: 1, InstanceID: "service", Paused: true})
	})
	if wrong, err := Connect(t.Context(), path, "old"); err == nil {
		wrong.Close()
		t.Fatal("attached to another service instance")
	}
	session, err := Connect(t.Context(), path, "service")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if duplicate, err := Connect(t.Context(), path, "service"); !errors.Is(err, filelock.ErrBusy) {
		if duplicate != nil {
			duplicate.Close()
		}
		t.Fatalf("second menu owner error = %v", err)
	}
	ready, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		session.Run(t.Context(), nil, nil, func(state State) {
			if !state.Paused || !state.Pause || !state.Next || state.Previous || !state.Stop {
				t.Errorf("unexpected state from paused service: %+v", state)
			}
			select {
			case <-ready:
			default:
				close(ready)
			}
		})
	}()
	await(t, ready, "initial menu")
	closeServer()
	await(t, done, "menu exit after external shutdown")
	if mutations.Load() != 0 {
		t.Fatal("menu polling sent a mutation")
	}
}

func TestMenuStateGatesUnavailableControls(t *testing.T) {
	for _, status := range []control.Status{{SetupRequired: true}, {Busy: true}} {
		state := menuState(status)
		if state.Pause || state.Previous || state.Next || !state.Stop {
			t.Fatalf("controls enabled while unavailable: %+v", state)
		}
	}
	state := menuState(control.Status{Paused: true, HistoryCursor: 2, Uncertain: true})
	if state.Pause || !state.Previous || !state.Next || !state.Stop {
		t.Fatalf("uncertainty should allow explicit navigation and stop: %+v", state)
	}
	if paused := menuState(control.Status{Paused: true}); paused.allows(Pause) || !paused.allows(Resume) {
		t.Fatal("a stale Pause selection can send Resume")
	}
}

func TestDetachedSessionNeverActsOnAReplacementService(t *testing.T) {
	var replacement atomic.Bool
	path, _ := testSocket(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/status" {
			t.Errorf("stale menu sent mutation %s %s", r.Method, r.URL.Path)
		}
		instance := "observed"
		if replacement.Load() {
			instance = "new"
		}
		_ = json.NewEncoder(w).Encode(control.Status{Version: 1, InstanceID: instance})
	})
	session, err := Connect(t.Context(), path, "observed")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	actions := make(chan Action, 1)
	replacement.Store(true)
	actions <- Next
	done := make(chan struct{})
	go func() { defer close(done); session.Run(t.Context(), actions, nil, func(State) {}) }()
	await(t, done, "stale menu exit")
}
