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

func TestOmarchyFitFailuresDoNotInvokeSetter(t *testing.T) {
	for _, tt := range []struct {
		name, monitorScript string
		badCache, corrupt   bool
	}{
		{"missing monitor command", "", false, false},
		{"no monitors", "printf '[]'", false, false},
		{"multiple monitors", `printf '[{"name":"A","width":8,"height":4,"scale":1},{"name":"B","width":8,"height":4,"scale":1}]'`, false, false},
		{"rotated monitor", `printf '[{"name":"A","width":8,"height":4,"scale":1,"transform":1}]'`, false, false},
		{"cache blocked", `printf '[{"name":"A","width":8,"height":4,"scale":1}]'`, true, false},
		{"partial image", `printf '[{"name":"A","width":8,"height":4,"scale":1}]'`, false, true},
		{"monitor changed", `if test -f "$PANORA_TEST_QUERY"; then
printf '[{"name":"A","width":4,"height":8,"scale":1}]'
else
printf called > "$PANORA_TEST_QUERY"
printf '[{"name":"A","width":8,"height":4,"scale":1}]'
fi`, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "called")
			t.Setenv("PANORA_TEST_MARKER", marker)
			t.Setenv("PANORA_TEST_QUERY", filepath.Join(t.TempDir(), "query"))
			cache := t.TempDir()
			if tt.badCache {
				cache = filepath.Join(cache, "blocked")
				if err := os.WriteFile(cache, []byte("file"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			setTestCache(t, cache)
			scripts := map[string]string{"omarchy": `printf called > "$PANORA_TEST_MARKER"`}
			if tt.monitorScript != "" {
				scripts["hyprctl"] = tt.monitorScript
			}
			fakeBackendCommands(t, scripts)
			path := filepath.Join(t.TempDir(), "source.png")
			writeTestImage(t, path, 2, 2)
			if tt.corrupt {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data[:33], 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := (OmarchyBackend{}).SetWallpaper(context.Background(), path, DisplayOptions{}); err == nil {
				t.Fatal("expected preparation failure")
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("setter called or marker check failed: %v", err)
			}
		})
	}
}

func TestOmarchyFailures(t *testing.T) {
	t.Run("missing executable", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if err := (OmarchyBackend{}).SetWallpaper(context.Background(), "/image.png", DisplayOptions{Fit: "crop"}); !errors.Is(err, exec.ErrNotFound) {
			t.Fatalf("got %v, want executable not found", err)
		}
	})
	t.Run("nonzero exit", func(t *testing.T) {
		fakeOmarchy(t, "echo 'session unavailable' >&2\nexit 7\n")
		err := (OmarchyBackend{}).SetWallpaper(context.Background(), "/image.png", DisplayOptions{Fit: "crop"})
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 || !strings.Contains(err.Error(), "session unavailable") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		fakeOmarchy(t, "exec /bin/sleep 10\n")
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if err := (OmarchyBackend{}).SetWallpaper(ctx, "/image.png", DisplayOptions{Fit: "crop"}); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("got %v, want deadline exceeded", err)
		}
	})
}

func TestOmarchyGuardUsesInstalledNativeWallpaperProtocol(t *testing.T) {
	for _, old := range []bool{true, false} {
		name := "sequenced IPC"
		if old {
			name = "Omarchy 4.5"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
			log := filepath.Join(t.TempDir(), "setter")
			t.Setenv("PANORA_TEST_LOG", log)
			if old {
				t.Setenv("PANORA_TEST_LEGACY", "1")
			}
			fakeBackendCommands(t, map[string]string{
				"omarchy": `printf 'cli:%s' "$*" >"$PANORA_TEST_LOG"`,
				"omarchy-shell": `
case "$2" in
themeIntroStatus) test "$3" = 33:44 && printf ready ;;
themeIntroBusy)
  if test "$PANORA_TEST_LEGACY" = 1; then
    printf 'Function not found.\n' >&2
    exit 1
  fi
  printf ready ;;
switchBackground) printf 'ipc:%s' "$3" >"$PANORA_TEST_LOG"; printf ok ;;
esac`,
			})
			if !OmarchyIntroReady(t.Context(), "33:44") || OmarchyIntroReady(t.Context(), "33:45") {
				t.Fatal("native intro query did not use the active directory token")
			}
			target := "/cache/image.webp"
			ctx := WithOmarchyDispatchGuard(t.Context(), func() bool {
				nativeRelease, err := TryOmarchyThemeLock()
				if nativeRelease != nil {
					nativeRelease()
				}
				if err == nil {
					t.Fatal("theme switch could race the dispatch guard")
				}
				return true
			})
			backend := OmarchyBackend{}
			if err := backend.SetWallpaper(ctx, target, DisplayOptions{Fit: "crop"}); err != nil {
				t.Fatal(err)
			}
			value, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			want := "ipc:" + target
			if old {
				want = "cli:theme bg set " + target
			}
			if string(value) != want {
				t.Fatalf("unexpected native dispatch %q", value)
			}
			ctx = WithOmarchyDispatchGuard(t.Context(), func() bool { return false })
			if err := backend.SetWallpaper(ctx, "/cache/stale.webp", DisplayOptions{Fit: "crop"}); !errors.Is(err, ErrOmarchyThemeChanged) {
				t.Fatalf("outdated generation was not discarded: %v", err)
			}
			value, err = os.ReadFile(log)
			if err != nil || string(value) != want {
				t.Fatalf("outdated generation overwrote native apply: %q %v", value, err)
			}
		})
	}
}
