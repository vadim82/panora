package desktop

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNoctaliaFailures(t *testing.T) {
	t.Run("missing executable", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if err := (NoctaliaBackend{}).SetWallpaper(context.Background(), "/image.png", DisplayOptions{}); !errors.Is(err, exec.ErrNotFound) {
			t.Fatalf("got %v, want executable not found", err)
		}
	})
	t.Run("nonzero exit", func(t *testing.T) {
		fakeNoctalia(t, "echo 'session unavailable' >&2\nexit 7\n")
		err := (NoctaliaBackend{}).SetWallpaper(context.Background(), "/image.png", DisplayOptions{})
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 || !strings.Contains(err.Error(), "session unavailable") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		fakeNoctalia(t, "exec /bin/sleep 10\n")
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if err := (NoctaliaBackend{}).SetWallpaper(ctx, "/image.png", DisplayOptions{}); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("got %v, want deadline exceeded", err)
		}
	})
}

func TestNoctaliaRejectsUnsupportedDisplayOptions(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "called")
	t.Setenv("PANORA_TEST_MARKER", marker)
	fakeNoctalia(t, `printf called > "$PANORA_TEST_MARKER"`)
	for _, options := range []DisplayOptions{{Fit: "contain"}, {Fit: "crop"}, {Upscale: true}} {
		if err := (NoctaliaBackend{}).SetWallpaper(context.Background(), "/image.png", options); err == nil {
			t.Fatalf("accepted %+v", options)
		}
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("setter called or marker check failed: %v", err)
	}
}
