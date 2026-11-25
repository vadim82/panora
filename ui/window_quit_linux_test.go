package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/vadim82/panora/internal/control"
)

func TestDesktopQuitTargetsExistingWindowsOfAcknowledgedService(t *testing.T) {
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon is needed for an isolated session bus")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	process := exec.CommandContext(ctx, daemon, "--session", "--print-address=1", "--nofork")
	output, err := process.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { process.Process.Kill(); process.Wait() })
	busAddress := bufio.NewScanner(output)
	if !busAddress.Scan() {
		t.Fatalf("read private D-Bus session address: %v", busAddress.Err())
	}
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", busAddress.Text())
	bus, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()

	path := filepath.Join(t.TempDir(), "control.sock")
	if err := os.Chmod(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := control.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	var managed, editor, unrelated, reopened, accepted atomic.Int32
	registerWindow := func(path string, isManaged bool, count *atomic.Int32) func() {
		return serveDesktopQuit(ctx, path, isManaged, func() { count.Add(1) })
	}
	closeManaged := registerWindow(path, true, &managed)
	if closeManaged == nil {
		t.Fatal("register managed window")
	}
	t.Cleanup(closeManaged)
	closeEditor := registerWindow(path, false, &editor)
	if closeEditor == nil {
		t.Fatal("register connect-only window")
	}
	t.Cleanup(closeEditor)
	closeUnrelated := registerWindow(filepath.Join(t.TempDir(), "control.sock"), false, &unrelated)
	if closeUnrelated == nil {
		t.Fatal("register unrelated window")
	}
	t.Cleanup(closeUnrelated)
	var replaceWindow, replacementRegistered atomic.Bool
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/shutdown" || r.URL.Query().Get("instance") != "observed" {
			http.Error(w, `{"error":{"code":"instance_changed","message":"different instance"}}`, http.StatusConflict)
			return
		}
		accepted.Add(1)
		if replaceWindow.Load() {
			closeManaged()
			if closeReopened := registerWindow(path, true, &reopened); closeReopened != nil {
				t.Cleanup(closeReopened)
				replacementRegistered.Store(true)
			}
		}
		_ = json.NewEncoder(w).Encode(control.ShutdownResult{Version: 1, Stopping: true, InstanceID: "observed"})
	})}
	defer server.Close()
	go server.Serve(listener)

	if _, err := quitDesktopInstance(ctx, bus, path, "stale"); err == nil || managed.Load() != 0 || editor.Load() != 0 {
		t.Fatalf("stale request affected existing windows: err=%v managed=%d connect-only=%d", err, managed.Load(), editor.Load())
	}
	if _, err := quitDesktopInstance(ctx, bus, path, "observed"); err != nil {
		t.Fatal(err)
	}
	if managed.Load() != 1 || editor.Load() != 1 || unrelated.Load() != 0 {
		t.Fatalf("quit targeted wrong windows: managed=%d connect-only=%d unrelated=%d", managed.Load(), editor.Load(), unrelated.Load())
	}
	closeEditor()
	replaceWindow.Store(true)
	if _, err := quitDesktopInstance(ctx, bus, path, "observed"); err != nil {
		t.Fatal(err)
	}
	if !replacementRegistered.Load() || managed.Load() != 1 || editor.Load() != 1 || unrelated.Load() != 0 || reopened.Load() != 0 || accepted.Load() != 2 {
		t.Fatalf("quit targeted wrong windows after replacement: managed=%d connect-only=%d unrelated=%d reopened=%d accepted=%d", managed.Load(), editor.Load(), unrelated.Load(), reopened.Load(), accepted.Load())
	}
}
