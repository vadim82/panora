package appservice

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/filelock"
)

type missingJob struct{}

func (missingJob) Error() string { return "not loaded" }
func (missingJob) ExitCode() int { return 113 }

type sessionFixture struct {
	program, source           string
	mu                        sync.Mutex
	running, loaded, disabled bool
	generation                int
	starts, stops             int
	calls                     []string
	failure                   string
}

func newFixture(t *testing.T) (*Manager, *sessionFixture) {
	t.Helper()
	home := t.TempDir()
	executable := filepath.Join(home, "Applications with spaces", "Panora.app", "Contents", "MacOS", "panora")
	if err := os.MkdirAll(filepath.Dir(executable), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	manager, err := New(Options{Executable: executable, Home: home, Socket: filepath.Join(home, "run", "control.sock")})
	if err != nil {
		t.Fatal(err)
	}
	fixture := &sessionFixture{}
	manager.command = func(_ context.Context, args ...string) ([]byte, error) {
		fixture.mu.Lock()
		defer fixture.mu.Unlock()
		fixture.calls = append(fixture.calls, strings.Join(args, " "))
		if args[0] == fixture.failure {
			return nil, fmt.Errorf("injected %s failure", args[0])
		}
		switch args[0] {
		case "print":
			if !fixture.loaded {
				return nil, missingJob{}
			}
			return fmt.Appendf(nil, "path = %s\nprogram = %s\n", fixture.source, fixture.program), nil
		case "print-disabled":
			return fmt.Appendf(nil, `disabled services = { "%s" => %t }`, manager.options.Label, fixture.disabled), nil
		case "enable":
			fixture.disabled = false
		case "bootstrap":
			config, err := readAgent(args[2])
			if err != nil {
				return nil, err
			}
			fixture.loaded = true
			fixture.source = args[2]
			fixture.program = config.Arguments[0]
		case "bootout":
			fixture.loaded = false
			fixture.running = false
		case "kickstart":
			if !fixture.running {
				fixture.starts++
				fixture.generation++
				fixture.running = true
			}
		}
		return nil, nil
	}
	manager.probe = func(_ context.Context) (control.Status, error) {
		fixture.mu.Lock()
		defer fixture.mu.Unlock()
		if !fixture.running {
			return control.Status{}, errors.New("disconnected")
		}
		return control.Status{Version: 1, InstanceID: fmt.Sprint(fixture.generation), Paused: true}, nil
	}
	manager.shutdown = func(_ context.Context) (control.ShutdownResult, error) {
		fixture.mu.Lock()
		defer fixture.mu.Unlock()
		fixture.stops++
		fixture.running = false
		return control.ShutdownResult{Stopping: true}, nil
	}
	return manager, fixture
}

func TestConcurrentStartsReuseOneServiceAndStopStaysStopped(t *testing.T) {
	manager, fixture := newFixture(t)
	var group sync.WaitGroup
	for range 12 {
		group.Go(func() {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			state, err := manager.Start(ctx)
			if err != nil || !state.Running {
				t.Errorf("start: %+v %v", state, err)
			}
		})
	}
	group.Wait()
	if fixture.starts != 1 {
		t.Fatalf("%d starts", fixture.starts)
	}
	state, err := manager.Stop(t.Context())
	if err != nil || state.Running {
		t.Fatalf("stop: %+v %v", state, err)
	}
	for range 3 {
		if manager.Status(t.Context()).Running {
			t.Fatal("polling restarted the service")
		}
	}
	if fixture.starts != 1 || fixture.stops != 1 {
		t.Fatalf("unexpected lifecycle: %+v", fixture)
	}
	state, err = manager.Start(t.Context())
	if err != nil || !state.Running || fixture.starts != 2 {
		t.Fatalf("explicit restart: %+v %v", state, err)
	}
}

func TestLoginChangesKeepCurrentPlaybackAndReportSystemDisable(t *testing.T) {
	manager, fixture := newFixture(t)
	if _, err := manager.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{true, false, true} {
		state, err := manager.SetLogin(t.Context(), enabled)
		if err != nil || state.StartAtLogin != enabled || !state.Running {
			t.Fatalf("login %v: %+v %v", enabled, state, err)
		}
	}
	fixture.disabled = true
	if state := manager.Status(t.Context()); state.Platform != "darwin" || state.LoginState != "disabled_by_system" {
		t.Fatalf("state %+v", state)
	}
	if fixture.starts != 1 || fixture.stops != 0 {
		t.Fatal("login edit interrupted playback")
	}
	if _, err := manager.RemoveStartup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if fixture.running || fixture.loaded {
		t.Fatal("owned service was not removed")
	}
	if _, err := os.Stat(manager.registration); !os.IsNotExist(err) {
		t.Fatalf("registration remained: %v", err)
	}
}

func TestStartRejectsLiveIncompatibleOwnerWithoutKillingIt(t *testing.T) {
	manager, fixture := newFixture(t)
	if err := os.MkdirAll(manager.runtime, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manager.options.Socket, []byte("incompatible fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	release, err := filelock.Try(manager.options.Socket + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	_, err = manager.Start(t.Context())
	if err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("start: %v", err)
	}
	if fixture.starts != 0 || fixture.stops != 0 || fixture.loaded {
		t.Fatal("existing owner was changed")
	}
}

func TestFailedBootstrapAndInvalidRegistrationAreVisible(t *testing.T) {
	manager, fixture := newFixture(t)
	fixture.failure = "bootstrap"
	if _, err := manager.Start(t.Context()); err == nil || !strings.Contains(err.Error(), "bootstrap") {
		t.Fatalf("start: %v", err)
	}
	if fixture.starts != 0 {
		t.Fatal("started after failed registration")
	}
	if err := os.MkdirAll(filepath.Dir(manager.registration), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manager.registration, []byte("not plist"), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(manager.registration)
	if _, err := manager.SetLogin(t.Context(), false); err == nil {
		t.Fatal("invalid registration removed")
	}
	after, _ := os.ReadFile(manager.registration)
	if string(before) != string(after) {
		t.Fatal("unrecognized registration modified")
	}
}

func TestRestartWaitsForOwnerAndDoesNotReplaceExternalService(t *testing.T) {
	manager, fixture := newFixture(t)
	fixture.running = true
	if _, err := manager.Restart(t.Context()); err == nil {
		t.Fatal("external service was restarted")
	}
	if fixture.stops != 0 {
		t.Fatal("stopped external service")
	}
	fixture.running = false
	if _, err := manager.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	release, err := filelock.Try(manager.options.Socket + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	if _, err := manager.Restart(ctx); err == nil {
		t.Fatal("restart ignored a draining owner")
	}
	if fixture.starts != 1 {
		t.Fatal("started before previous owner released its lock")
	}
}

func TestMovedBundleRequiresExplicitRestartAndRefreshesRegistration(t *testing.T) {
	manager, fixture := newFixture(t)
	if _, err := manager.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.SetLogin(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	old := manager.options.Executable
	manager.options.Executable = filepath.Join(filepath.Dir(old), "new location", "panora")
	if state, err := manager.Start(t.Context()); err != nil || !state.NeedsRestart || fixture.starts != 1 {
		t.Fatalf("move: %+v %v", state, err)
	}
	if _, err := manager.Restart(t.Context()); err != nil {
		t.Fatal(err)
	}
	config, _, err := manager.readOwned(manager.registration)
	if err != nil || config.Arguments[0] != manager.options.Executable || fixture.starts != 2 {
		t.Fatalf("updated: %+v %v", config, err)
	}
}

func TestAgentEncodingAndUnsupportedEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.plist")
	original := agent{Label: "org.panora.test", Arguments: []string{"/Applications/Space & snow 雪/panora", "serve"}, Home: "/Users/A & B", RunAtLoad: true}
	if err := os.WriteFile(path, original.marshal(), 0600); err != nil {
		t.Fatal(err)
	}
	decoded, err := readAgent(path)
	if err != nil || decoded.Arguments[0] != original.Arguments[0] || !decoded.RunAtLoad {
		t.Fatalf("round trip: %+v %v", decoded, err)
	}
	modified := strings.Replace(string(original.marshal()), "<key>KeepAlive</key><false/>", "<key>KeepAlive</key><true/>", 1)
	if err := os.WriteFile(path, []byte(modified), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readAgent(path); err == nil {
		t.Fatal("edited restart policy was accepted")
	}
}
