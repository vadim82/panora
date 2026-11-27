//go:build darwin || linux

package appservice

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func homebrewFixtureKeg(t *testing.T, directory, version string) string {
	t.Helper()
	keg := filepath.Join(directory, "Cellar", "panora", version)
	for _, executable := range []string{keg + homebrewServiceSuffix, filepath.Join(keg, "bin", "panora")} {
		if err := os.MkdirAll(filepath.Dir(executable), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(executable, nil, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return keg
}

func TestHomebrewLinkReportsOnlyACompleteReplacementKeg(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	old := homebrewFixtureKeg(t, root, "1")
	newKeg := homebrewFixtureKeg(t, root, "2")
	link := filepath.Join(root, "opt", "panora")
	if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(link, "bin", "panora")
	executable := old + homebrewServiceSuffix
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	changed := watchHomebrewLink(ctx, launcher, filepath.Dir(old), executable, 5*time.Millisecond)
	for _, target := range []string{old, filepath.Join(root, "missing")} {
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		select {
		case path := <-changed:
			t.Fatalf("unexpected replacement %q", path)
		case <-time.After(20 * time.Millisecond):
		}
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(newKeg, link); err != nil {
		t.Fatal(err)
	}
	select {
	case path := <-changed:
		if path != newKeg+homebrewServiceSuffix {
			t.Fatalf("replacement = %q", path)
		}
	case <-time.After(time.Second):
		t.Fatal("no replacement detected")
	}
	if path := HomebrewLaunchPath(executable); path != launcher {
		t.Fatalf("launch path = %q, want %q", path, launcher)
	}
}

func TestHomebrewStartMigratesOnlyItsLegacyJob(t *testing.T) {
	for _, name := range []string{"running", "stopped", "external", "manual"} {
		t.Run(name, func(t *testing.T) {
			manager, fixture := newFixture(t)
			ctx := t.Context()
			root, err := filepath.EvalSymlinks(manager.options.Home)
			if err != nil {
				t.Fatal(err)
			}
			old := homebrewFixtureKeg(t, root, "1")
			newKeg := homebrewFixtureKeg(t, root, "2")
			link := filepath.Join(root, "opt", "panora")
			if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(newKeg, link); err != nil {
				t.Fatal(err)
			}
			if name != "manual" {
				manager.options.Executable = old + homebrewServiceSuffix
			}
			if name == "running" || name == "manual" {
				if _, err := manager.Start(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := manager.SetLogin(ctx, true); err != nil {
				t.Fatal(err)
			}
			if name == "external" {
				fixture.running = true
			}
			oldExecutable := manager.options.Executable
			manager.options.Executable = HomebrewLaunchPath(newKeg + homebrewServiceSuffix)
			if manager.options.Executable != filepath.Join(link, "bin", "panora") {
				t.Fatal("missing stable Homebrew launch path")
			}
			state, err := manager.Start(ctx)
			if err != nil || !state.Running || !state.StartAtLogin {
				t.Fatalf("start after upgrade: %+v, %v", state, err)
			}
			config, _, err := manager.readOwned(manager.registration)
			if err != nil {
				t.Fatal(err)
			}
			wantPath, wantStarts := manager.options.Executable, 1
			switch name {
			case "running":
				wantStarts = 2
			case "external":
				wantPath, wantStarts = oldExecutable, 0
			case "manual":
				wantPath = oldExecutable
			}
			if config.Arguments[0] != wantPath || fixture.starts != wantStarts {
				t.Fatalf("registration %s, %d starts, want %s and %d starts", config.Arguments[0], fixture.starts, wantPath, wantStarts)
			}
		})
	}
}
