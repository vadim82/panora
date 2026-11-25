package appservice

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/control"
)

type systemdFixture struct {
	socket    string
	mu        sync.Mutex
	child     *systemdChild
	starts    int
	login     string
	fragment  string
	status    string
	restart   string
	graphical string
	active    string
	pid       int
}

type systemdChild struct {
	pid    int
	exited chan struct{}
}

func testSystemdManager(t *testing.T) (*SystemdManager, *systemdFixture) {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "panora", "control.sock")
	fixture := &systemdFixture{socket: socket, login: "disabled", fragment: systemdFragment, restart: "no", graphical: "active"}
	return &SystemdManager{socket: socket, command: fixture.command(t), owns: func() bool { return true }}, fixture
}

func (f *systemdFixture) command(t *testing.T) func(context.Context, ...string) ([]byte, error) {
	t.Helper()
	return func(ctx context.Context, args ...string) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if args[0] == "show" && args[len(args)-1] == "graphical-session.target" {
			return []byte(f.graphical + "\n"), nil
		}
		if args[len(args)-1] != systemdUnit {
			return nil, fmt.Errorf("unexpected unit arguments: %v", args)
		}
		switch args[0] {
		case "show":
			pid, active := 0, "inactive"
			if f.child != nil {
				select {
				case <-f.child.exited:
				default:
					pid, active = f.child.pid, "active"
				}
			}
			if f.status != "" && f.starts > 0 {
				active = "failed"
			}
			if f.active != "" {
				active = f.active
			}
			if f.pid > 0 {
				pid = f.pid
			}
			return fmt.Appendf(nil, "LoadState=loaded\nFragmentPath=%s\nDropInPaths=\nExecStart={ path=/usr/bin/panora ; argv[]=/usr/bin/panora serve --pause-for-setup --omarchy-session ; }\nActiveState=%s\nMainPID=%d\nUnitFileState=%s\nRestart=%s\nStatusText=%s\n",
				f.fragment, active, pid, f.login, f.restart, f.status), nil
		case "start":
			f.starts++
			if f.status == "" {
				child, err := runSystemdChild(t, f.socket, strconv.Itoa(f.starts), control.APIVersion)
				if err != nil {
					return nil, err
				}
				f.child = child
			}
		case "enable":
			f.login = "enabled"
		case "disable":
			f.login = "disabled"
		default:
			return nil, fmt.Errorf("unexpected systemd command: %v", args)
		}
		return nil, nil
	}
}

func runSystemdChild(t *testing.T, socket, instance string, version int) (*systemdChild, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestSystemdServiceChild$")
	cmd.Env = append(os.Environ(), "PANORA_SYSTEMD_TEST_SOCKET="+socket, "PANORA_SYSTEMD_TEST_INSTANCE="+instance,
		"PANORA_SYSTEMD_TEST_VERSION="+strconv.Itoa(version))
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	child := &systemdChild{pid: cmd.Process.Pid, exited: make(chan struct{})}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		select {
		case <-child.exited:
		case <-time.After(2 * time.Second):
			t.Error("service fixture did not exit")
		}
	})
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(out)
		ready <- scanner.Scan() && scanner.Text() == "ready"
		_ = cmd.Wait()
		close(child.exited)
	}()
	select {
	case ok := <-ready:
		if !ok {
			return nil, fmt.Errorf("service fixture did not report ready")
		}
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("timed out waiting for service fixture")
	}
	return child, nil
}

func TestSystemdServiceChild(t *testing.T) {
	path := os.Getenv("PANORA_SYSTEMD_TEST_SOCKET")
	if path == "" {
		return
	}
	instance := os.Getenv("PANORA_SYSTEMD_TEST_INSTANCE")
	version, err := strconv.Atoi(os.Getenv("PANORA_SYSTEMD_TEST_VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := control.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	stop := make(chan struct{})
	var stopOnce sync.Once
	server := &http.Server{
		ReadHeaderTimeout: time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/v1/status":
				_ = json.NewEncoder(w).Encode(control.Status{Version: version, InstanceID: instance})
			case r.Method == http.MethodPost && r.URL.Path == "/v1/shutdown":
				_ = json.NewEncoder(w).Encode(control.ShutdownResult{Version: version, Stopping: true, InstanceID: instance})
				_ = http.NewResponseController(w).Flush()
				stopOnce.Do(func() { close(stop) })
			default:
				http.NotFound(w, r)
			}
		}),
	}
	go func() { _ = server.Serve(listener) }()
	fmt.Println("ready")
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSystemdLifecycle(t *testing.T) {
	manager, fixture := testSystemdManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	initial := manager.Status(ctx)
	if !initial.Available || initial.Running || initial.StartAtLogin {
		t.Fatalf("initial state: %+v", initial)
	}
	if _, err := os.Stat(filepath.Dir(manager.socket)); !os.IsNotExist(err) {
		t.Fatalf("status changed the runtime directory: %v", err)
	}
	other := *manager
	type result struct {
		state State
		err   error
	}
	results := make(chan result, 2)
	for _, client := range []*SystemdManager{manager, &other} {
		go func() {
			state, err := client.Start(ctx)
			results <- result{state, err}
		}()
	}
	for range 2 {
		r := <-results
		if r.err != nil || !r.state.Managed || r.state.InstanceID != "1" || r.state.StartAtLogin {
			t.Fatalf("concurrent Start: %+v, %v", r.state, r.err)
		}
	}
	if fixture.starts != 1 {
		t.Fatalf("concurrent starts: %d", fixture.starts)
	}
	if state, err := manager.SetLogin(ctx, true); err != nil || !state.StartAtLogin || state.InstanceID != "1" {
		t.Fatalf("enable: %+v, %v", state, err)
	}
	if state, err := manager.Restart(ctx); err != nil || !state.Managed || state.InstanceID != "2" || !state.StartAtLogin {
		t.Fatalf("restart: %+v, %v", state, err)
	}
	if state, err := manager.SetLogin(ctx, false); err != nil || state.StartAtLogin || state.InstanceID != "2" {
		t.Fatalf("disable: %+v, %v", state, err)
	}
	if state, err := manager.Stop(ctx); err != nil || state.Running {
		t.Fatalf("stop: %+v, %v", state, err)
	}
	for range 4 {
		if state := manager.Status(ctx); state.Running || state.StartAtLogin || fixture.starts != 2 {
			t.Fatalf("polling after stop: %+v", state)
		}
	}
	if state, err := manager.SetLogin(ctx, true); err != nil || !state.StartAtLogin || state.Running {
		t.Fatalf("enable while stopped: %+v, %v", state, err)
	}
	if _, err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if state, err := manager.RemoveStartup(ctx); err != nil || state.Running || state.StartAtLogin {
		t.Fatalf("prepare removal: %+v, %v", state, err)
	}
}

func TestSystemdOtherSocketOwners(t *testing.T) {
	for _, version := range []int{control.APIVersion, control.APIVersion + 1} {
		t.Run(strconv.Itoa(version), func(t *testing.T) {
			manager, fixture := testSystemdManager(t)
			fixture.login = "enabled"
			child, err := runSystemdChild(t, fixture.socket, "separate", version)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			state, err := manager.Start(ctx)
			if version == control.APIVersion {
				if err != nil || !state.Running || state.InstanceID != "separate" || state.Managed {
					t.Fatalf("external service reuse: %+v, %v", state, err)
				}
			} else if err == nil || state.Running || !state.StartBlocked {
				t.Fatalf("incompatible service: %+v, %v", state, err)
			}
			for _, change := range []func(context.Context) (State, error){manager.Stop, manager.Restart} {
				if state, err := change(ctx); err == nil || state.Managed {
					t.Fatalf("unsafe operation: %+v, %v", state, err)
				}
			}
			state, err = manager.RemoveStartup(ctx)
			if version == control.APIVersion {
				if err != nil || state.StartAtLogin || state.RemoveBlocked || state.InstanceID != "separate" {
					t.Fatalf("remove packaged registration with a foreground service: %+v, %v", state, err)
				}
			} else if err == nil || !state.RemoveBlocked || !state.StartAtLogin {
				t.Fatalf("unsafe remove: %+v, %v", state, err)
			}
			_, peer, _ := control.StatusWithPeer(ctx, fixture.socket)
			if peer.PID != child.pid || fixture.starts != 0 {
				t.Fatalf("external owner changed: %+v, starts=%d", peer, fixture.starts)
			}
			if _, err := control.ShutdownPeer(ctx, fixture.socket, control.Peer{PID: child.pid + 1, UID: peer.UID}); err == nil {
				t.Fatal("sent a shutdown request to a different peer")
			}
			if _, after, _ := control.StatusWithPeer(ctx, fixture.socket); after != peer {
				t.Fatalf("socket changed after wrong-peer shutdown: %+v", after)
			}
		})
	}
}

func TestSystemdWaitsForManagerReady(t *testing.T) {
	manager, fixture := testSystemdManager(t)
	fixture.active = "activating"
	child, err := runSystemdChild(t, fixture.socket, "activating", control.APIVersion)
	if err != nil {
		t.Fatal(err)
	}
	fixture.child = child
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if state := manager.Status(ctx); !state.Running || state.Managed || !state.RemoveBlocked {
		t.Fatalf("unit still starting: %+v", state)
	}
	deadline, stop := context.WithTimeout(ctx, 50*time.Millisecond)
	defer stop()
	if state, err := manager.Start(deadline); err == nil || state.Managed {
		t.Fatalf("accepted API before systemd confirmed READY: %+v, %v", state, err)
	}
	fixture.active = "active"
	if state, err := manager.Start(ctx); err != nil || !state.Managed || fixture.starts != 0 {
		t.Fatalf("did not join the ready startup: %+v, %v", state, err)
	}
}

func TestSystemdStuckPackagedProcess(t *testing.T) {
	manager, fixture := testSystemdManager(t)
	fixture.active, fixture.pid, fixture.login = "active", os.Getpid(), "enabled"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, change := range []func(context.Context) (State, error){manager.Start, manager.Stop, manager.Restart, manager.RemoveStartup} {
		state, err := change(ctx)
		if err == nil || !strings.Contains(err.Error(), stopCommand) || !strings.Contains(err.Error(), journalCommand) ||
			!state.StartBlocked || !state.RemoveBlocked || !state.StartAtLogin || fixture.starts != 0 {
			t.Fatalf("stuck process was not reported safely: %+v, %v", state, err)
		}
	}
	if _, err := runSystemdChild(t, fixture.socket, "foreground", control.APIVersion); err != nil {
		t.Fatal(err)
	}
	if state, err := manager.RemoveStartup(ctx); err == nil || !state.RemoveBlocked || !state.StartAtLogin {
		t.Fatalf("remove unexpectedly modified an active unit with a foreign peer: %+v, %v", state, err)
	}
}

func TestSystemdRestartReturnsReplacementState(t *testing.T) {
	manager, fixture := testSystemdManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	if _, err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	command := manager.command
	var replacement *systemdChild
	manager.command = func(ctx context.Context, args ...string) ([]byte, error) {
		raw, err := command(ctx, args...)
		if err != nil || args[0] != "show" || args[len(args)-1] != systemdUnit || replacement != nil {
			return raw, err
		}
		select {
		case <-fixture.child.exited:
			replacement, err = runSystemdChild(t, fixture.socket, "replacement", control.APIVersion)
			return append(raw, []byte("Job=1\n")...), err
		default:
			return raw, nil
		}
	}
	if state, err := manager.Restart(ctx); err == nil || state.InstanceID != "replacement" || state.Managed || fixture.starts != 1 {
		t.Fatalf("restart failed without reporting the replacement: %+v, %v", state, err)
	}
}

func TestSystemdReportsStartupFailure(t *testing.T) {
	manager, fixture := testSystemdManager(t)
	fixture.status = startupStatus + "OMARCHY_PATH is missing"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	fixture.graphical = "inactive"
	if _, err := manager.Start(ctx); err == nil || !strings.Contains(err.Error(), "graphical-session.target") || fixture.starts != 0 {
		t.Fatalf("headless start: %v, starts=%d", err, fixture.starts)
	}
	fixture.graphical = "active"
	if state, err := manager.Start(ctx); err == nil || !strings.Contains(state.Error, "OMARCHY_PATH") || state.Running {
		t.Fatalf("startup: %+v, %v", state, err)
	}
	fixture.fragment = "/home/fixture/.config/systemd/user/panora.service"
	if state, err := manager.SetLogin(ctx, true); err == nil || state.Available || state.StartAtLogin {
		t.Fatalf("foreign registration: %+v, %v", state, err)
	}
}
