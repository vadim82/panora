//go:build linux || darwin

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/filelock"
	"github.com/vadim82/panora/internal/playback"
)

func TestServiceProcess(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "panora")
	build := exec.Command("go", "build", "-race", "-o", executable, "./cmd/panora")
	build.Dir = filepath.Join("..", "..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build entry point: %v\n%s", err, output)
	}
	t.Run("graceful stop", func(t *testing.T) {
		testStopCLIProcessAcknowledgementCleanupAndInflightCancellation(t, executable)
	})
	t.Run("JSON parser and operational errors", func(t *testing.T) {
		for _, args := range [][]string{
			{"next", "--unknown", "--json"},
			{"exclusions", "remove", "invalid", "--json"},
			{"next", "--socket", shortSocket(t), "--json"},
			{"config", "check", "--config", filepath.Join(t.TempDir(), "missing.toml"), "--json"},
		} {
			cmd := exec.Command(executable, args...)
			var out, errOut bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &errOut
			err := cmd.Run()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 || errOut.Len() != 0 {
				t.Fatalf("exit/streams for %v: %v %q %q", args, err, &out, &errOut)
			}
			var response struct {
				Version int            `json:"version"`
				Error   *control.Error `json:"error"`
			}
			if err := json.Unmarshal(out.Bytes(), &response); err != nil || response.Error == nil || response.Version != 1 {
				t.Fatalf("invalid JSON: %s %v", &out, err)
			}
		}
	})
	t.Run("help colors follow terminal and environment", func(t *testing.T) {
		script := `
import json, os, pty, subprocess, sys
exe = sys.argv[1]
base = dict(os.environ, TERM="xterm-256color")
base.pop("NO_COLOR", None)
def terminal(env, args):
    master, slave = pty.openpty()
    try:
        child = subprocess.Popen([exe] + args, stdout=slave, stderr=subprocess.PIPE, env=env)
        os.close(slave)
        slave = None
        chunks = []
        while True:
            try:
                chunk = os.read(master, 65536)
            except OSError:
                break
            if not chunk:
                break
            chunks.append(chunk)
        _, errors = child.communicate(timeout=5)
        if child.returncode or errors:
            raise RuntimeError(errors)
        return b"".join(chunks).decode()
    finally:
        os.close(master)
        if slave is not None:
            os.close(slave)
colored = terminal(base, ["--help"])
plain = terminal(dict(base, NO_COLOR=""), ["--help"])
dumb = terminal(dict(base, TERM="dumb"), ["--help"])
json_help = terminal(base, ["status", "--json", "--help"])
pipe = subprocess.check_output([exe, "--help"], env=base).decode()
print(json.dumps(dict(colored=colored, plain=plain, dumb=dumb, pipe=pipe, json_help=json_help)))
`
		output, err := exec.Command("python3", "-c", script, executable).CombinedOutput()
		if err != nil {
			t.Fatalf("terminal help: %v %s", err, output)
		}
		var modes map[string]string
		if err := json.Unmarshal(output, &modes); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(modes["colored"], "\x1b[") {
			t.Fatal("interactive help has no color")
		}
		for _, mode := range []string{"plain", "dumb", "pipe", "json_help"} {
			if strings.Contains(modes[mode], "\x1b[") {
				t.Fatalf("color leaked into %s", mode)
			}
		}
	})

	t.Run("missing service and removed commands", func(t *testing.T) {
		options, socket := serviceFixture(t)
		setTestCache(t, t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("XDG_STATE_HOME", t.TempDir())
		statePath, err := playback.DefaultStatePath()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(statePath), 0700); err != nil {
			t.Fatal(err)
		}
		options.Request.StatePath = statePath
		// Every desktop invocation would leave evidence, including readiness probes.
		marker := filepath.Join(t.TempDir(), "desktop-call")
		fakeBackendCommands(t, map[string]string{"noctalia": "echo called > '" + marker + "'\nexit 9"})
		before := []byte(`{"version":1}`)
		if err := os.WriteFile(options.Request.StatePath, before, 0600); err != nil {
			t.Fatal(err)
		}
		for _, action := range []string{"status", "next", "previous", "pause", "resume"} {
			output, err := exec.Command(executable, action, "--socket", socket).CombinedOutput()
			if err == nil || !strings.Contains(string(output), "start panora serve") {
				t.Fatalf("%s: expected missing-service guidance: %v %s", action, err, output)
			}
		}
		for _, args := range [][]string{{"rotate"}, {"next", "--standalone"}, {"next", "--config", options.Request.ConfigPath}} {
			if output, err := exec.Command(executable, args...).CombinedOutput(); err == nil {
				t.Fatalf("removed entry point accepted %v: %s", args, output)
			}
		}
		for _, args := range [][]string{{"--help"}, {"next", "--help"}, {"__complete", "", ""}} {
			output, err := exec.Command(executable, args...).CombinedOutput()
			if err != nil || strings.Contains(string(output), "standalone") || strings.Contains(string(output), "rotate") {
				t.Fatalf("obsolete help/completion for %v: %v %s", args, err, output)
			}
		}
		after, err := os.ReadFile(options.Request.StatePath)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("client changed state: %s %v", after, err)
		}
		if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("client invoked desktop")
		}
		if _, err := os.Stat(socket); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("client created socket")
		}
	})

	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGKILL} {
		t.Run(sig.String(), func(t *testing.T) {
			options, socket := serviceFixture(t)
			fakeBackendCommands(t, map[string]string{"noctalia": `case "$1 $2" in
"msg wallpaper-get") printf '/before.jpg\n' ;;
"config export") printf '[wallpaper]\nenabled = true\n' ;;
"msg wallpaper-set") test -f "$3" ;;
*) exit 9 ;;
esac`})
			start := func() (*exec.Cmd, <-chan error, *bytes.Buffer) {
				args := []string{"serve", "--paused", "--socket", socket, "--config", options.Request.ConfigPath, "--state-file", options.Request.StatePath, "--interval", "1h"}
				cmd := exec.Command(executable, args...)
				output := new(bytes.Buffer)
				cmd.Stdout, cmd.Stderr = output, output
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				done := make(chan error, 1)
				go func() { done <- cmd.Wait(); close(done) }()
				t.Cleanup(func() {
					select {
					case <-done:
						return
					default:
					}
					cmd.Process.Kill()
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("helper did not stop")
					}
				})
				return cmd, done, output
			}
			cmd, done, _ := start()
			awaitService(t, socket)
			statusCommand := func(action string) control.Status {
				t.Helper()
				output, err := exec.Command(executable, action, "--socket", socket, "--json").CombinedOutput()
				if err != nil {
					t.Fatalf("%s: %v %s", action, err, output)
				}
				var status control.Status
				if err := json.Unmarshal(output, &status); err != nil {
					t.Fatal(err)
				}
				return status
			}
			awaitDiscovery(t, socket)
			first := statusCommand("next")
			if !first.Paused || first.HistoryLength != 1 {
				t.Fatalf("manual next via real entrypoint failed: %+v", first)
			}
			second := statusCommand("next")
			if second.HistoryLength != 2 || second.Current.Path == first.Current.Path {
				t.Fatal("repeat avoidance failed")
			}
			if previous := statusCommand("previous"); previous.Current.Path != first.Current.Path {
				t.Fatal("previous failed")
			}
			if forward := statusCommand("next"); forward.HistoryLength != 2 || forward.Current.Path != second.Current.Path {
				t.Fatal("forward failed")
			}
			if status := statusCommand("resume"); status.Paused || status.NextAt == nil {
				t.Fatal("resume failed")
			}
			statusCommand("pause")
			duplicate := exec.Command(executable, "serve", "--paused", "--config", options.Request.ConfigPath, "--state-file", options.Request.StatePath, "--socket", shortSocket(t))
			if output, err := duplicate.CombinedOutput(); err == nil || !strings.Contains(string(output), "another service") {
				t.Fatalf("duplicate accepted: %v %s", err, output)
			}

			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if sig == syscall.SIGKILL {
					if err == nil {
						t.Fatal("SIGKILL unexpectedly exited successfully")
					}
				} else if err != nil {
					t.Fatalf("signal shutdown: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("service did not stop on signal")
			}
			for _, path := range []string{options.Request.StatePath + ".lock", options.Request.StatePath + ".rotation.lock", socket + ".lock"} {
				release, err := filelock.Try(path)
				if err != nil {
					t.Fatalf("lock survived service exit: %v", err)
				}
				release()
			}
			if sig != syscall.SIGKILL {
				if _, err := os.Lstat(socket); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("socket survived clean shutdown: %v", err)
				}
			}
			state, err := playback.LoadState(options.Request.StatePath)
			if err != nil || !state.Paused || state.CurrentPath() != second.Current.Path {
				t.Fatalf("shutdown lost accepted history: %+v %v", state, err)
			}
			if sig == syscall.SIGKILL {
				restarted, restartDone, _ := start()
				status := awaitService(t, socket)
				if status.HistoryLength != 2 || !status.Paused || status.Current.Path != second.Current.Path {
					t.Fatal("restart did not recover history and stale socket")
				}
				if _, err := control.Request(context.Background(), socket, "next"); err != nil {
					t.Fatal(err)
				}
				restarted.Process.Signal(syscall.SIGTERM)
				select {
				case err := <-restartDone:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("restarted helper did not stop")
				}
			}
		})
	}
}
