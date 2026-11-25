package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPackagedSpaceObserverHasDedicatedExecutable(t *testing.T) {
	root := t.TempDir()
	companion := filepath.Join(root, "Panora.app", "Contents", "MacOS", macOSHelperName)
	if _, err := macOSSpaceHelper(companion); err == nil {
		t.Fatal("missing observer fell back to the main app identity")
	}
	observer := filepath.Join(root, "Panora.app", "Contents", "Helpers", "Panora Space Observer.app", "Contents", "MacOS", macOSHelperName)
	if err := os.MkdirAll(filepath.Dir(observer), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(observer, []byte("#!/bin/sh\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := macOSSpaceHelper(companion); err == nil {
		t.Fatal("nonexecutable observer accepted")
	}
	if err := os.Chmod(observer, 0700); err != nil {
		t.Fatal(err)
	}
	if got, err := macOSSpaceHelper(companion); err != nil || got != observer {
		t.Fatalf("packaged observer=%q error=%v", got, err)
	}
	standalone := filepath.Join(root, "bin", macOSHelperName)
	if got, err := macOSSpaceHelper(standalone); err != nil || got != standalone {
		t.Fatalf("standalone observer=%q error=%v", got, err)
	}
}

func TestSpaceObserverProtocolAndCancellation(t *testing.T) {
	for _, scenario := range []struct {
		name, output string
		change       bool
	}{
		{"event", "ready\nchanged\n", true},
		{"bad-readiness", "unexpected\n", false},
		{"bad-event", "ready\ninvalid\n", false},
		{"unterminated-overflow", strings.Repeat("x", 1024), false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fake-helper")
			// A real independent child exercises output pipes and process joining;
			// it has no AppKit or wallpaper behavior.
			script := "#!/usr/bin/env python3\nimport sys,time\nsys.stdout.write(" + "\"" + strings.ReplaceAll(scenario.output, "\n", "\\n") + "\");sys.stdout.flush()\ntime.sleep(60)\n"
			if err := os.WriteFile(path, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			ready, changed := false, false
			err := watchSpaces(ctx, path, func() { ready = true }, func() { changed = true; cancel() })
			if ready != strings.HasPrefix(scenario.output, "ready\n") || changed != scenario.change || err == nil {
				t.Fatalf("ready=%t changed=%t error=%v", ready, changed, err)
			}
			if scenario.change && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation not preserved: %v", err)
			}
			if !scenario.change && errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("invalid protocol was not bounded promptly: %v", err)
			}
		})
	}
}

func TestSpaceObserverReportsUnreadyStartup(t *testing.T) {
	for _, scenario := range []struct {
		name, body, diagnostic string
		timeout                bool
	}{
		{"early-exit", "raise SystemExit(1)", "exited before readiness", false},
		{"no-readiness", "import time; time.sleep(60)", "did not report readiness within 5s", true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fake-helper")
			if err := os.WriteFile(path, []byte("#!/usr/bin/env python3\n"+scenario.body+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), spaceObserverStartupTimeout+3*time.Second)
			defer cancel()
			calls := 0
			err := watchSpaces(ctx, path, func() { t.Error("unexpected readiness") }, func() { calls++ })
			if calls != 0 || ctx.Err() != nil || err == nil || !strings.Contains(err.Error(), scenario.diagnostic) || errors.Is(err, context.DeadlineExceeded) != scenario.timeout {
				t.Fatalf("observer startup: calls=%d parent=%v error=%v", calls, ctx.Err(), err)
			}
		})
	}
}
