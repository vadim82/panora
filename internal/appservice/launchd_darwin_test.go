package appservice

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt-in: creates only a uniquely labeled agent in the current GUI session,
// with private temporary files and a fixture that never touches wallpapers.
func TestMacLaunchAgentProcessLifecycle(t *testing.T) {
	if os.Getenv("PANORA_LAUNCHD_TEST") != "1" {
		t.Skip("set PANORA_LAUNCHD_TEST=1 in an authorized graphical session")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	home, err := os.MkdirTemp("/tmp", "panora-launchd-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(home); err != nil {
			t.Error(err)
		}
	})
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	result, err := exec.CommandContext(ctx, python, "-c", "import sys; print(sys.executable)").Output()
	if err != nil {
		t.Fatal(err)
	}
	python = strings.TrimSpace(string(result))
	fixture, err := os.ReadFile("../../tools/fixtures/app_service.py")
	if err != nil {
		t.Fatal(err)
	}
	fixturePath := filepath.Join(home, "fixture.py")
	if err := os.WriteFile(fixturePath, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(home, "Applications with spaces", "Panora.app", "Contents", "MacOS", "panora")
	if err := os.MkdirAll(filepath.Dir(executable), 0700); err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	script := "#!/bin/sh\nexec " + quote(python) + " " + quote(fixturePath) + " \"$@\"\n"
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	label := "org.panora.test." + filepath.Base(home)
	manager, err := New(Options{Executable: executable, Home: home, Socket: filepath.Join(home, "run", "control.sock"), Label: label})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := manager.RemoveStartup(cleanup); err != nil {
			t.Error(err)
			// The fixture itself created this unique label; remove it even if a product
			// ownership check fails, before deleting the private fixture source files.
			_, _ = launchctl(cleanup, "bootout", manager.target)
		}
	})
	first, err := manager.Start(ctx)
	if err != nil || !first.Running || !first.Managed || first.StartAtLogin {
		t.Fatalf("first start: %+v %v", first, err)
	}
	second, err := manager.Start(ctx)
	if err != nil || second.InstanceID != first.InstanceID {
		t.Fatalf("duplicate start: %+v %v", second, err)
	}
	for _, enabled := range []bool{true, false} {
		state, err := manager.SetLogin(ctx, enabled)
		if err != nil || state.StartAtLogin != enabled || state.InstanceID != first.InstanceID {
			t.Fatalf("login %v: %+v %v", enabled, state, err)
		}
	}
	if _, err := manager.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
	if state := manager.Status(ctx); state.Running {
		t.Fatalf("stopped service restarted: %+v", state)
	}
	if _, err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	restarted, err := manager.Restart(ctx)
	if err != nil || !restarted.Running || restarted.InstanceID == first.InstanceID {
		t.Fatalf("restart: %+v %v", restarted, err)
	}
	removed, err := manager.RemoveStartup(ctx)
	if err != nil || removed.Running || removed.Managed || removed.StartAtLogin {
		t.Fatalf("cleanup: %+v %v", removed, err)
	}
}
