package playback

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/filelock"
	"github.com/vadim82/panora/internal/sources/local"
)

func configurationFixture(t *testing.T, body string) (*Service, string, *firstRunBackend) {
	t.Helper()
	path := writeConfigBody(t, body)
	backend := &firstRunBackend{}
	request := Request{ConfigPath: path, StatePath: filepath.Join(t.TempDir(), "state.db"), CachePath: t.TempDir(), Source: local.Provider{}, Options: desktop.DisplayOptions{Fit: "auto"}}
	request.InspectCapabilities = func(_ context.Context, name string) (desktop.Capabilities, error) {
		return testDesktopCapabilities(name), nil
	}
	request.ResolveBackend = func(context.Context, string) (desktop.WallpaperBackend, error) { return backend, nil }
	service := openTestService(t, request, func(context.Context) (desktop.WallpaperBackend, error) { return backend, nil }, time.Hour, true)
	return service, path, backend
}

func settings(t *testing.T, service *Service) Configuration {
	t.Helper()
	config, err := service.Configuration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func requireConfigError(t *testing.T, err error, code string) {
	t.Helper()
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != code {
		t.Fatalf("error=%v want %s", err, code)
	}
}

func TestFolderEnrollmentRevisionIdentityAndLiveDiscovery(t *testing.T) {
	service, path, backend := configurationFixture(t, "# keep saved interval distinct from override\nversion=1\n[filters]\nmin_width=2\nmin_height=2\n")
	initial := settings(t, service)
	if initial.TOML[0] != '#' || initial.Revision != configRevision([]byte(initial.TOML)) || initial.Effective.Interval != "1h0m0s" || initial.Rules.MinWidth != 2 {
		t.Fatalf("snapshot=%+v", initial)
	}
	service.StartDiscovery()
	root := filepath.Join(t.TempDir(), "Wallpapers")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	writeScanImage(t, filepath.Join(root, "image.png"), 2, 2)
	added, err := service.AddFolder(t.Context(), AddFolderRequest{Revision: initial.Revision, Path: root + "/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(added.Folders) != 1 || added.Folders[0].Name != "Wallpapers" || !added.Folders[0].Enabled || added.Folders[0].ID == "Wallpapers" || added.Revision == initial.Revision {
		t.Fatalf("added=%+v", added)
	}
	awaitCondition(t, func() bool {
		s := service.Status()
		return !s.SetupRequired && len(s.Providers) == 1 && s.Providers[0].Inputs[0].State == "ready"
	})
	if backend.calls.Load() != 0 || !service.Status().Paused {
		t.Fatal("enrollment changed desktop or pause")
	}
	// Duplicate selection returns the existing identity and exact saved revision.
	duplicate, err := service.AddFolder(t.Context(), AddFolderRequest{Revision: added.Revision, Path: root})
	if err != nil || duplicate.Revision != added.Revision || duplicate.Folders[0].ID != added.Folders[0].ID {
		t.Fatalf("duplicate=%+v %v", duplicate, err)
	}
	_, err = service.AddFolder(t.Context(), AddFolderRequest{Revision: initial.Revision, Path: t.TempDir()})
	requireConfigError(t, err, "config_conflict")
	sameName := filepath.Join(t.TempDir(), "Wallpapers") // Unavailable folders remain enrolled with health errors.
	second, err := service.AddFolder(t.Context(), AddFolderRequest{Revision: added.Revision, Path: sameName})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Folders) != 2 || second.Folders[0].ID == second.Folders[1].ID || second.Folders[0].Name != second.Folders[1].Name {
		t.Fatalf("identities=%+v", second.Folders)
	}
	awaitCondition(t, func() bool {
		s := service.Status()
		return len(s.Providers[0].Inputs) == 2 && s.Providers[0].Inputs[0].State == "ready" && s.Providers[0].Inputs[1].Failure != nil
	})
	saved, _, err := LoadConfig(path)
	if err != nil || saved.Providers[0].Folders[0].ID != added.Folders[0].ID || saved.Playback.Interval != "30m" {
		t.Fatalf("saved=%+v %v", saved, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("permissions=%v %v", info, err)
	}
	temps, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".config-*.toml"))
	if err != nil || len(temps) != 0 {
		t.Fatalf("temporary files=%v %v", temps, err)
	}
	// Same running playback path can use the new folder, with a fake desktop only.
	if _, err := service.Action(t.Context(), "next"); err != nil {
		t.Fatalf("action=%v status=%+v saved=%s", err, service.Status(), settings(t, service).TOML)
	}
	if backend.calls.Load() != 1 {
		t.Fatalf("desktop calls=%d", backend.calls.Load())
	}
	service.Close()
	restarted := openTestService(t, service.request, service.resolve, time.Hour, false)
	restored := settings(t, restarted)
	if restored.Revision != second.Revision || restored.Folders[0].ID != added.Folders[0].ID || !restarted.Status().Paused || restarted.Status().Current == nil || backend.calls.Load() != 1 {
		t.Fatalf("restart=%+v status=%+v", restored, restarted.Status())
	}
}

func TestFolderEnrollmentPreservesSettingsOnFailures(t *testing.T) {
	service, path, _ := configurationFixture(t, "version=1\n")
	original := settings(t, service)
	for _, root := range []string{"relative/path", "/bad\x00path", "/" + strings.Repeat("a", 4096)} {
		_, err := service.AddFolder(t.Context(), AddFolderRequest{Revision: original.Revision, Path: root})
		requireConfigError(t, err, "invalid_request")
	}
	// An observed direct external edit invalidates the reviewed revision.
	external := []byte("version=1\n# external edit\n")
	if err := os.WriteFile(path, external, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := service.AddFolder(t.Context(), AddFolderRequest{Revision: original.Revision, Path: t.TempDir()})
	requireConfigError(t, err, "config_conflict")
	if err := replaceConfig(t.Context(), path, original.Revision, []byte("version=1\n")); err == nil {
		t.Fatal("stale staged write succeeded")
	}
	if data, _ := os.ReadFile(path); string(data) != string(external) {
		t.Fatal("external settings replaced")
	}
	invalid := []byte("version=123\n")
	if err := os.WriteFile(path, invalid, 0600); err != nil {
		t.Fatal(err)
	}
	_, err = service.Configuration(t.Context())
	requireConfigError(t, err, "config_invalid")
	_, err = service.AddFolder(t.Context(), AddFolderRequest{Revision: original.Revision, Path: t.TempDir()})
	requireConfigError(t, err, "config_invalid")
	if data, _ := os.ReadFile(path); string(data) != string(invalid) {
		t.Fatal("invalid settings replaced")
	}
}

func TestFolderEnrollmentDoesNotEnableDisabledInputsOrFollowConfigSymlink(t *testing.T) {
	service, path, _ := configurationFixture(t, strings.Replace(localTOML(t.TempDir()), "enabled = true", "enabled = false", 1))
	before := settings(t, service)
	_, err := service.AddFolder(t.Context(), AddFolderRequest{Revision: before.Revision, Path: t.TempDir()})
	requireConfigError(t, err, "provider_disabled")
	if settings(t, service).Revision != before.Revision {
		t.Fatal("disabled inputs altered")
	}
	target := path + ".saved"
	if err := os.Rename(path, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	_, err = service.AddFolder(t.Context(), AddFolderRequest{Revision: before.Revision, Path: t.TempDir()})
	requireConfigError(t, err, "config_write_failed")
	if data, _ := os.ReadFile(target); string(data) != before.TOML {
		t.Fatal("symlink target overwritten")
	}
}

func TestFolderEnrollmentSerializesWritersAndCancelsLockWait(t *testing.T) {
	service, path, _ := configurationFixture(t, "version=1\n")
	before := settings(t, service)
	start := make(chan struct{})
	var group sync.WaitGroup
	var successes int
	var mu sync.Mutex
	for range 8 {
		group.Go(func() {
			<-start
			_, err := service.AddFolder(t.Context(), AddFolderRequest{Revision: before.Revision, Path: t.TempDir()})
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				successes++
				return
			}
			var typed *Error
			if !errors.As(err, &typed) || (typed.Code != "busy" && typed.Code != "config_conflict") {
				t.Errorf("concurrent error=%v", err)
			}
		})
	}
	close(start)
	group.Wait()
	if successes != 1 || len(settings(t, service).Folders) != 1 {
		t.Fatalf("successful writers=%d", successes)
	}
	release, err := filelock.Acquire(t.Context(), path+".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err = service.AddFolder(ctx, AddFolderRequest{Revision: settings(t, service).Revision, Path: t.TempDir()})
	requireConfigError(t, err, "config_write_failed")
	if len(settings(t, service).Folders) != 1 {
		t.Fatal("canceled save changed settings")
	}
}

func TestFolderEnrollmentWriteFailurePreservesSavedAndRunningSettings(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	service, path, backend := configurationFixture(t, "version=1\n")
	before := settings(t, service)
	directory := filepath.Dir(path)
	if err := os.Chmod(directory, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(directory, 0700) })
	_, err := service.AddFolder(t.Context(), AddFolderRequest{Revision: before.Revision, Path: t.TempDir()})
	requireConfigError(t, err, "config_write_failed")
	if settings(t, service).Revision != before.Revision || !service.Status().SetupRequired || backend.calls.Load() != 0 {
		t.Fatal("failed write changed saved or running settings")
	}
}
