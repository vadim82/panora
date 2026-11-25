//go:build linux || darwin

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/filelock"
	"github.com/vadim82/panora/internal/playback"
)

func testStopCLIProcessAcknowledgementCleanupAndInflightCancellation(t *testing.T, executable string) {
	t.Helper()
	for _, mode := range []string{"accepted", "inflight", "disconnected"} {
		t.Run(mode, func(t *testing.T) {
			options, socket := serviceFixture(t)
			setTestCache(t, t.TempDir())
			marker := filepath.Join(t.TempDir(), "setter-started")
			setter := `test -f "$3"`
			if mode == "inflight" {
				setter = ": > '" + marker + "'\nexec /bin/sleep 30"
			}
			script := fmt.Sprintf(`case "$1 $2" in
"msg wallpaper-get") printf '/before.jpg\n' ;;
"config export") printf '[wallpaper]\nenabled = true\n' ;;
"msg wallpaper-set") %s ;;
*) exit 9 ;;
esac`, setter)
			fakeBackendCommands(t, map[string]string{"noctalia": script})
			cmd := exec.Command(executable, "serve", "--paused", "--socket", socket, "--config", options.Request.ConfigPath, "--state-file", options.Request.StatePath)
			cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			t.Cleanup(func() { _ = cmd.Process.Kill() })
			awaitService(t, socket)
			awaitDiscovery(t, socket)
			configBefore, err := os.ReadFile(options.Request.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			var current *control.Entry
			if mode != "inflight" {
				status, err := control.Request(t.Context(), socket, "next")
				if err != nil {
					t.Fatal(err)
				}
				current = status.Current
			} else {
				actionDone := make(chan struct{})
				go func() { defer close(actionDone); _, _ = control.Request(t.Context(), socket, "next") }()
				deadline := time.Now().Add(5 * time.Second)
				for {
					if _, err := os.Stat(marker); err == nil {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("setter never entered")
					}
					time.Sleep(10 * time.Millisecond)
				}
				t.Cleanup(func() {
					select {
					case <-actionDone:
					case <-time.After(5 * time.Second):
						t.Error("action did not join")
					}
				})
			}
			if mode == "disconnected" {
				connection, err := net.Dial("unix", socket)
				if err != nil {
					t.Fatal(err)
				}
				_, err = io.WriteString(connection, "POST /v1/shutdown HTTP/1.1\r\nHost: panora\r\nContent-Length: 0\r\n\r\n")
				_ = connection.Close()
				if err != nil {
					t.Fatal(err)
				}
			} else {
				out, err := exec.Command(executable, "stop", "--socket", socket, "--json").CombinedOutput()
				var ack control.ShutdownResult
				if err != nil || json.Unmarshal(out, &ack) != nil || !ack.Stopping || ack.Version != 1 {
					t.Fatalf("stop=%v %s", err, out)
				}
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("stop did not join in-flight work")
			}
			if _, err := os.Lstat(socket); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("socket survived=%v", err)
			}
			for _, path := range []string{options.Request.StatePath + ".lock", options.Request.StatePath + ".rotation.lock", socket + ".lock"} {
				release, err := filelock.Try(path)
				if err != nil {
					t.Fatal(err)
				}
				release()
			}
			state, err := playback.LoadState(options.Request.StatePath)
			if err != nil {
				t.Fatal(err)
			}
			if current != nil && state.CurrentPath() != current.Path {
				t.Fatalf("accepted history changed=%+v", state)
			}
			if mode == "inflight" && state.Pending == nil {
				t.Fatal("uncertain in-flight request was discarded")
			}
			configAfter, err := os.ReadFile(options.Request.ConfigPath)
			if err != nil || string(configBefore) != string(configAfter) {
				t.Fatal("shutdown changed preferences")
			}
			// A repeated request never starts a service or acquires state ownership.
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if _, err := control.Shutdown(ctx, socket); err == nil {
				t.Fatal("stopped service acknowledged another request")
			}
		})
	}
}
