package desktop

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/filelock"
)

func setCurrentBackground(t *testing.T, target string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	link := filepath.Join(home, ".local", "state", "omarchy", "current", "background")
	if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
		t.Fatal(err)
	}
	if target != "" {
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	return link
}

func cacheArtifact(t *testing.T, dir string, id int, size int) string {
	t.Helper()
	path := filepath.Join(dir, fmt.Sprintf("%064x.png", id))
	if err := os.WriteFile(path, make([]byte, size), 0600); err != nil {
		t.Fatal(err)
	}
	at := time.Unix(int64(id), 0)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFitCacheBudgetsAndProtectedPaths(t *testing.T) {
	for _, tt := range []struct {
		name  string
		files int
		bytes int64
	}{{"file count", 4, 1000}, {"bytes", 100, 40}} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			current := cacheArtifact(t, dir, 1, 10)
			source := cacheArtifact(t, dir, 2, 10)
			old := cacheArtifact(t, dir, 3, 10)
			candidate := cacheArtifact(t, dir, 4, 10)
			recent := cacheArtifact(t, dir, 5, 10)
			setCurrentBackground(t, current)
			temporary := filepath.Join(dir, ".fit-12345.png")
			unknown := filepath.Join(dir, "original.png")
			for _, path := range []string{temporary, unknown} {
				if err := os.WriteFile(path, []byte("keep originals"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			folder := filepath.Join(dir, fmt.Sprintf("%064x.png", 6))
			if err := os.Mkdir(folder, 0700); err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(dir, fmt.Sprintf("%064x.png", 7))
			if err := os.Symlink(unknown, alias); err != nil {
				t.Fatal(err)
			}
			if err := pruneFitCache(context.Background(), dir, tt.files, tt.bytes, source, candidate); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{current, source, candidate, recent, unknown, folder, alias} {
				if _, err := os.Lstat(path); err != nil {
					t.Fatalf("protected/unmanaged path removed: %s: %v", path, err)
				}
			}
			for _, path := range []string{old, temporary} {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("unused artifact remains: %s: %v", path, err)
				}
			}
		})
	}
}

func TestFitCacheUnsafeCleanupFailsWithoutRemovingCurrent(t *testing.T) {
	for _, name := range []string{"pinned over budget", "dangling current", "canceled"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			current := cacheArtifact(t, dir, 1, 10)
			candidate := cacheArtifact(t, dir, 2, 10)
			link := setCurrentBackground(t, current)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if name == "dangling current" {
				if err := os.Remove(link); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(dir, "missing.png"), link); err != nil {
					t.Fatal(err)
				}
			}
			if name == "canceled" {
				cancel()
			}
			if err := pruneFitCache(ctx, dir, 1, 10, candidate); err == nil {
				t.Fatal("unsafe cleanup succeeded")
			}
			for _, path := range []string{current, candidate} {
				if _, err := os.Stat(path); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestFitCacheCanonicalPaths(t *testing.T) {
	dir := t.TempDir()
	current := cacheArtifact(t, dir, 1, 10)
	old := cacheArtifact(t, dir, 2, 10)
	alias := filepath.Join(t.TempDir(), "cache")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	setCurrentBackground(t, filepath.Join(alias, filepath.Base(current)))
	if err := pruneFitCache(context.Background(), alias, 1, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(current); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old file remains: %v", err)
	}
}

func TestOmarchyCacheBoundedAcrossApplicationsAndFailure(t *testing.T) {
	link := setCurrentBackground(t, "")
	cache := setTestCache(t, os.Getenv("HOME"))
	failure := filepath.Join(t.TempDir(), "fail")
	t.Setenv("PANORA_TEST_FAIL", failure)
	fakeBackendCommands(t, map[string]string{
		"hyprctl": `printf '[{"name":"A","width":4,"height":4,"scale":1}]'`,
		"omarchy": `test -f "$4" || exit 8
test ! -e "$HOME/.local/state/omarchy/current/background" || test -f "$HOME/.local/state/omarchy/current/background" || exit 9
test ! -e "$PANORA_TEST_FAIL" || exit 7
/bin/ln -snf "$4" "$HOME/.local/state/omarchy/current/background"`,
	})
	source := filepath.Join(t.TempDir(), "source.png")
	var current string
	for i := 0; i < fitCacheMaxFiles+5; i++ {
		img := image.NewRGBA(image.Rect(0, 0, 2, 2))
		img.Set(0, 0, color.RGBA{R: uint8(i), A: 255})
		f, err := os.Create(source)
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, img)
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("write source: %v %v", err, closeErr)
		}
		if i == fitCacheMaxFiles+4 {
			if err := os.WriteFile(failure, nil, 0600); err != nil {
				t.Fatal(err)
			}
		}
		err = (OmarchyBackend{}).SetWallpaper(context.Background(), source, DisplayOptions{})
		if i == fitCacheMaxFiles+4 {
			if err == nil {
				t.Fatal("failed setter was accepted")
			}
			if _, err := os.Stat(current); err != nil {
				t.Fatalf("previous current removed after failure: %v", err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
		resolved, err := filepath.EvalSymlinks(link)
		if err != nil {
			t.Fatal(err)
		}
		if i == fitCacheMaxFiles+4 && resolved != current {
			t.Fatal("failed setter changed current")
		}
		current = resolved
		entries, err := os.ReadDir(filepath.Join(cache, "panora", "rendered"))
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, entry := range entries {
			if fitCacheName.MatchString(entry.Name()) {
				count++
			}
		}
		if count > fitCacheMaxFiles {
			t.Fatalf("cache grew to %d files", count)
		}
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatal("original removed")
	}
}

func TestOmarchyFitPreservesDisplayedAndPendingAtByteLimit(t *testing.T) {
	const width, height = 1600, 900
	link := setCurrentBackground(t, "")
	setTestCache(t, os.Getenv("HOME"))
	source := filepath.Join(t.TempDir(), "source.png")
	writeTestImage(t, source, 2, 2)
	ctx := context.Background()
	current, err := prepareContainedImage(ctx, source, width, height, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(current, link); err != nil {
		t.Fatal(err)
	}
	oldest := time.Unix(1, 0)
	if err := os.Chtimes(current, oldest, oldest); err != nil {
		t.Fatal(err)
	}
	displayed, err := os.Stat(current)
	if err != nil {
		t.Fatal(err)
	}
	// Start just under the real byte limit with two entries. A new PNG
	// exceeds the remaining 64 bytes and exercises the post-render prune.
	cache := filepath.Dir(current)
	old := cacheArtifact(t, cache, 2, 0)
	if err := os.Truncate(old, fitCacheMaxBytes-displayed.Size()-64); err != nil {
		t.Fatal(err)
	}

	f, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	input := image.NewGray(image.Rect(0, 0, 2, 2))
	input.SetGray(0, 0, color.Gray{Y: 255})
	encodeErr := png.Encode(f, input)
	if closeErr := f.Close(); encodeErr != nil || closeErr != nil {
		t.Fatalf("encode source: %v; close: %v", encodeErr, closeErr)
	}
	received := filepath.Join(t.TempDir(), "received")
	t.Setenv("PANORA_TEST_RECEIVED", received)
	fakeBackendCommands(t, map[string]string{
		"hyprctl": fmt.Sprintf(`printf '[{"name":"A","width":%d,"height":%d,"scale":1}]'`, width, height),
		"omarchy": `test -s "$4" || exit 8
test -s "$HOME/.local/state/omarchy/current/background" || exit 9
printf '%s' "$4" > "$PANORA_TEST_RECEIVED"`,
	})
	if err := (OmarchyBackend{}).SetWallpaper(ctx, source, DisplayOptions{}); err != nil {
		t.Fatal(err)
	}

	pending, err := os.ReadFile(received)
	if err != nil || string(pending) == current || filepath.Dir(string(pending)) != cache {
		t.Fatalf("pending = %q, error = %v", pending, err)
	}
	linked, err := os.Stat(link)
	if err != nil {
		t.Fatalf("stat displayed wallpaper: %v", err)
	}
	if !os.SameFile(linked, displayed) {
		t.Fatal("setter changed the displayed wallpaper")
	}
	prepared, err := os.Stat(string(pending))
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Size() <= 64 || displayed.Size()+prepared.Size() > fitCacheMaxBytes {
		t.Fatalf("displayed uses %d bytes and pending uses %d bytes", displayed.Size(), prepared.Size())
	}
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unprotected artifact remains: %v", err)
	}
}

func TestOmarchyCacheLockCancellation(t *testing.T) {
	setCurrentBackground(t, "")
	cache := setTestCache(t, os.Getenv("HOME"))
	fakeBackendCommands(t, map[string]string{"omarchy": "exit 9"})
	release, err := filelock.Try(filepath.Join(cache, "panora", "rendered", ".lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	beforeDispatch := func() error {
		t.Error("dispatch started while waiting for the Fit cache")
		return errors.New("unexpected dispatch")
	}
	err = (OmarchyBackend{}).SetWallpaperWithPreparation(t.Context(), ctx, "/unused.png", DisplayOptions{}, beforeDispatch)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want canceled cache wait", err)
	}
}

func TestOmarchyCacheLockHeldThroughSetter(t *testing.T) {
	setCurrentBackground(t, "")
	cache := setTestCache(t, os.Getenv("HOME"))
	marker := filepath.Join(t.TempDir(), "setting")
	ready := filepath.Join(t.TempDir(), "release")
	t.Setenv("PANORA_TEST_SETTING", marker)
	t.Setenv("PANORA_TEST_RELEASE", ready)
	fakeBackendCommands(t, map[string]string{
		"hyprctl": `printf '[{"name":"A","width":4,"height":4,"scale":1}]'`,
		"omarchy": `printf called > "$PANORA_TEST_SETTING"
while test ! -e "$PANORA_TEST_RELEASE"; do /bin/sleep 0.01; done
test -f "$4"`,
	})
	source := filepath.Join(t.TempDir(), "source.png")
	writeTestImage(t, source, 2, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- (OmarchyBackend{}).SetWallpaper(ctx, source, DisplayOptions{}) }()
	finished := false
	defer func() {
		cancel()
		if !finished {
			<-done
		}
	}()
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("setter did not start")
		}
		time.Sleep(time.Millisecond)
	}
	if release, err := filelock.Try(filepath.Join(cache, "panora", "rendered", ".lock")); !errors.Is(err, filelock.ErrBusy) {
		if release != nil {
			release()
		}
		t.Fatalf("cache unlocked while setter runs: %v", err)
	}
	if err := os.WriteFile(ready, nil, 0600); err != nil {
		t.Fatal(err)
	}
	err := <-done
	finished = true
	if err != nil {
		t.Fatal(err)
	}
}
