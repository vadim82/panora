package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOmarchyBarSetupRegistersOnceAndPreservesChoice(t *testing.T) {
	home := t.TempDir()
	b := omarchyBarSetup{
		source: filepath.Join(home, "packaged-plugin"),
		parent: filepath.Join(home, ".config", "omarchy", "plugins"),
	}
	rescanned, enabled := false, false
	readsAfterRescan := 0
	readsBeforeRescan := 0
	b.run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		switch name + " " + strings.Join(args, " ") {
		case "omarchy-shell shell listPlugins":
			if rescanned {
				readsAfterRescan++
			} else {
				readsBeforeRescan++
				if readsBeforeRescan == 1 {
					return nil, errors.New("shell plugin catalog is not ready")
				}
			}
			if readsAfterRescan == 1 {
				return nil, errors.New("shell is rescanning plugins")
			}
			if readsAfterRescan > 2 {
				return []byte(`[{"id":"io.github.vadim82.panora"}]`), nil
			}
			return []byte(`[{"id":"omarchy.clock"}]`), nil
		case "omarchy-shell shell rescanPlugins":
			rescanned = true
		case "omarchy-shell shell enablePlugin io.github.vadim82.panora {}":
			if !rescanned || enabled {
				t.Fatal("enabled before discovery or enabled twice")
			}
			enabled = true
			return []byte("ok"), nil
		default:
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if !b.available() {
		t.Fatal("unregistered profile did not qualify for setup")
	}
	if err := b.complete(ctx); err != nil {
		t.Fatal(err)
	}
	if dest, err := os.Readlink(filepath.Join(b.parent, omarchyBarID)); err != nil || dest != b.source {
		t.Fatalf("plugin registration = %q, %v", dest, err)
	}
	if !enabled || b.available() {
		t.Fatalf("enabled = %v, setup available = %v", enabled, b.available())
	}
	if err := b.complete(ctx); err != nil || !enabled {
		t.Fatalf("rerun changed enabled state: enabled = %v, err = %v", enabled, err)
	}
}

func TestOmarchyBarSetupPreservesLegacyRegistration(t *testing.T) {
	b := omarchyBarSetup{
		parent: t.TempDir(),
		run: func(_ context.Context, command string, args ...string) ([]byte, error) {
			if command != "omarchy-shell" || strings.Join(args, " ") != "shell listPlugins" {
				t.Fatalf("unexpected mutation: %s %v", command, args)
			}
			return []byte(`[{"id":"vadim82.panora","enabled":true}]`), nil
		},
	}
	if err := b.complete(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(b.parent, omarchyBarID)); !os.IsNotExist(err) {
		t.Fatalf("setup registered duplicate plugin: %v", err)
	}
}

func TestOmarchyBarSetupRetriesAfterFailure(t *testing.T) {
	home := t.TempDir()
	setup := omarchyBarSetup{source: filepath.Join(home, "package"), parent: home}
	var discovered bool
	var shouldFail = true
	setup.run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		switch name + " " + strings.Join(args, " ") {
		case "omarchy-shell shell listPlugins":
			if discovered {
				return []byte(`[{"id":"io.github.vadim82.panora"}]`), nil
			}
			return []byte(`[]`), nil
		case "omarchy-shell shell rescanPlugins":
			discovered = true
		case "omarchy-shell shell enablePlugin io.github.vadim82.panora {}":
			if shouldFail {
				return []byte("unknown"), nil
			}
			return []byte("ok"), nil
		default:
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		return nil, nil
	}

	if err := setup.complete(t.Context()); err == nil {
		t.Fatal("setup unexpectedly succeeded")
	}
	if _, err := os.Lstat(filepath.Join(home, omarchyBarID)); !os.IsNotExist(err) {
		t.Fatalf("failed registration prevented a retry: %v", err)
	}
	shouldFail = false
	discovered = false
	if err := setup.complete(t.Context()); err != nil {
		t.Fatal(err)
	}
	if dest, err := os.Readlink(filepath.Join(home, omarchyBarID)); err != nil || dest != setup.source {
		t.Fatalf("plugin registration = %q, %v", dest, err)
	}
}

func TestOmarchyBarSetupKeepsRegistrationWhenEnablementIsUnconfirmed(t *testing.T) {
	home := t.TempDir()
	setup := omarchyBarSetup{source: filepath.Join(home, "package"), parent: home}
	setup.run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		switch args[1] {
		case "listPlugins":
			if _, err := os.Lstat(filepath.Join(home, omarchyBarID)); err == nil {
				return []byte(`[{"id":"io.github.vadim82.panora"}]`), nil
			}
			return []byte(`[]`), nil
		case "rescanPlugins":
			return nil, nil
		case "enablePlugin":
			return nil, errors.New("shell acknowledgement lost")
		default:
			t.Fatalf("unexpected command: %v", args)
			return nil, nil
		}
	}
	if err := setup.complete(t.Context()); err == nil {
		t.Fatal("setup unexpectedly confirmed enablement")
	}
	if dest, err := os.Readlink(filepath.Join(home, omarchyBarID)); err != nil || dest != setup.source {
		t.Fatalf("unconfirmed enablement lost its registration: %q, %v", dest, err)
	}
}

func TestOmarchyBarRefreshTracksPackageEntryPoint(t *testing.T) {
	dir := t.TempDir()
	setup := omarchyBarSetup{
		source:        filepath.Join(dir, "package"),
		parent:        dir,
		refreshRecord: filepath.Join(dir, "refresh"),
	}
	if err := os.Mkdir(setup.source, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(setup.source, "manifest.json")
	install := func(entry string) {
		t.Helper()
		if err := os.WriteFile(manifest, []byte(`{"entryPoints":{"barWidget":"`+entry+`"}}`), 0644); err != nil {
			t.Fatal(err)
		}
	}
	var rescans int
	var rescanErr error
	var enabled bool
	var loaded string
	var sourceChecks int
	setup.run = func(_ context.Context, command string, args ...string) ([]byte, error) {
		switch command + " " + strings.Join(args, " ") {
		case "omarchy-shell shell listPlugins":
			if enabled {
				return []byte(`[{"id":"io.github.vadim82.panora","enabled":true}]`), nil
			}
			return []byte(`[{"id":"io.github.vadim82.panora","enabled":false}]`), nil
		case "omarchy-shell shell rescanPlugins":
			rescans++
			return nil, rescanErr
		case "omarchy-shell io.github.vadim82.panora widgetSource":
			sourceChecks++
			return []byte("file:///home/test/plugins/" + omarchyBarID + "/" + loaded + "\n"), nil
		default:
			t.Fatalf("unexpected shell command: %s %v", command, args)
			return nil, nil
		}
	}
	registration := filepath.Join(dir, omarchyBarID)
	if err := os.Symlink(setup.source, registration); err != nil {
		t.Fatal(err)
	}

	const oldEntry = "releases/r175.abcdef-1/BarWidget.qml"
	const newEntry = "releases/r176.abcdef-1/BarWidget.qml"
	install(oldEntry)
	if err := setup.complete(t.Context()); err != nil || rescans != 1 {
		t.Fatalf("initial refresh = %d, %v", rescans, err)
	}
	if err := setup.complete(t.Context()); err != nil || rescans != 1 {
		t.Fatalf("same package refresh = %d, %v", rescans, err)
	}
	if sourceChecks != 0 {
		t.Fatal("disabled widget was expected to load")
	}
	install(newEntry)
	enabled = true
	loaded = oldEntry
	rescanErr = errors.New("IPC unavailable")
	if err := setup.complete(t.Context()); !errors.Is(err, rescanErr) || rescans != 2 {
		t.Fatalf("failed refresh = %d, %v", rescans, err)
	}
	rescanErr = nil
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := setup.complete(ctx); !errors.Is(err, context.DeadlineExceeded) || rescans != 3 || sourceChecks == 0 {
		t.Fatalf("still loaded old widget = %d rescans, %d source checks, %v", rescans, sourceChecks, err)
	}
	if last, err := os.ReadFile(setup.refreshRecord); err != nil || string(last) != oldEntry {
		t.Fatalf("failed refresh replaced prior record: %q, %v", last, err)
	}
	loadOnRescan := setup.run
	setup.run = func(ctx context.Context, command string, args ...string) ([]byte, error) {
		if strings.Join(args, " ") == "shell rescanPlugins" {
			loaded = newEntry
		}
		return loadOnRescan(ctx, command, args...)
	}
	if err := setup.complete(t.Context()); err != nil || rescans != 4 {
		t.Fatalf("retry refresh = %d, %v", rescans, err)
	}
	if last, err := os.ReadFile(setup.refreshRecord); err != nil || string(last) != newEntry {
		t.Fatalf("record after refresh = %q, %v", last, err)
	}
	if target, err := os.Readlink(registration); err != nil || target != setup.source {
		t.Fatalf("refresh altered registration: %q, %v", target, err)
	}
	if err := setup.complete(t.Context()); err != nil || rescans != 4 {
		t.Fatalf("current widget refreshed again = %d, %v", rescans, err)
	}
}

func TestOmarchyBarRefreshPreservesUserOwnedRegistration(t *testing.T) {
	for _, kind := range []string{"directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			setup := omarchyBarSetup{
				source:        filepath.Join(dir, "package"),
				parent:        dir,
				refreshRecord: filepath.Join(dir, "refresh"),
				run: func(_ context.Context, command string, args ...string) ([]byte, error) {
					t.Fatalf("unexpected shell command: %s %v", command, args)
					return nil, nil
				},
			}
			registration := filepath.Join(dir, omarchyBarID)
			var err error
			switch kind {
			case "directory":
				err = os.Mkdir(registration, 0755)
			case "symlink":
				err = os.Symlink(filepath.Join(dir, "checkout"), registration)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := setup.complete(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
