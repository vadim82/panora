package desktop

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMacOSWallpaperArguments(t *testing.T) {
	requireMacOS(t)
	for _, tt := range []struct {
		options DisplayOptions
		want    string
	}{
		{DisplayOptions{}, "contain\nfalse\n"},
		{DisplayOptions{Fit: "auto"}, "contain\nfalse\n"},
		{DisplayOptions{Fit: "contain", Upscale: true}, "contain\ntrue\n"},
		{DisplayOptions{Fit: "crop"}, "crop\nfalse\n"},
	} {
		t.Run(tt.want, func(t *testing.T) {
			record := filepath.Join(t.TempDir(), "args")
			t.Setenv("PANORA_TEST_ARGUMENTS", record)
			fakeBackendCommands(t, map[string]string{macOSHelperName: `printf '%s\n' "$@" > "$PANORA_TEST_ARGUMENTS"
printf 'ok\n'`})
			path := "/Pictures/space ' quotes $(literal) 日本語.png"
			if err := (MacOSBackend{}).SetWallpaper(context.Background(), path, tt.options); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(record)
			if err != nil {
				t.Fatal(err)
			}
			if want := "set\n" + path + "\n" + tt.want; string(got) != want {
				t.Fatalf("arguments = %q, want %q", got, want)
			}
		})
	}
}

func TestMacOSGeometryCommandReadsBoundedBackingPixels(t *testing.T) {
	for _, test := range []struct {
		body, state string
		displays    int
	}{
		{`[{"name":"Retina","width":3024,"height":1964,"scale":2,"transform":0}]`, "known", 1},
		{`[]`, "unavailable", 0},
		{`[{"name":"A","width":1920,"height":1080,"scale":1},{"name":"B","width":1920,"height":1080,"scale":1}]`, "ambiguous", 2},
	} {
		fakeBackendCommands(t, map[string]string{macOSHelperName: "[ \"$*\" = geometry ] || exit 9\nprintf '%s' '" + test.body + "'"})
		geometry, err := macOSGeometry(t.Context())
		if err != nil || geometry.State != test.state || len(geometry.Displays) != test.displays {
			t.Fatalf("geometry=%+v %v", geometry, err)
		}
		if test.displays == 1 && geometry.Displays[0].Width != 3024 {
			t.Fatal("backing pixels were multiplied by scale")
		}
	}
	for _, body := range []string{"ready", `[{"name":"bad","width":3024,"height":1964,"scale":0}]`} {
		fakeBackendCommands(t, map[string]string{macOSHelperName: "printf '%s' '" + body + "'"})
		if _, err := macOSGeometry(t.Context()); err == nil {
			t.Fatalf("accepted malformed/old helper geometry=%s", body)
		}
	}
}

func TestMacOSHelperFailures(t *testing.T) {
	t.Run("missing helper", func(t *testing.T) {
		fakeBackendCommands(t, nil)
		if _, err := runMacOSHelper(context.Background(), "probe"); !errors.Is(err, exec.ErrNotFound) {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("nonzero exit", func(t *testing.T) {
		fakeBackendCommands(t, map[string]string{macOSHelperName: "echo 'desktop unavailable' >&2\nexit 7"})
		_, err := runMacOSHelper(context.Background(), "probe")
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 || !strings.Contains(err.Error(), "desktop unavailable") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("output bounds", func(t *testing.T) {
		for _, script := range []string{"printf '%070000d' 0", "printf '%05000d' 0 >&2"} {
			fakeBackendCommands(t, map[string]string{macOSHelperName: script})
			if _, err := runMacOSHelper(t.Context(), "probe"); err == nil || !strings.Contains(err.Error(), "output limit exceeded") {
				t.Fatalf("unbounded helper output=%v", err)
			}
		}
	})

	t.Run("timeout", func(t *testing.T) {
		fakeBackendCommands(t, map[string]string{macOSHelperName: "exec /bin/sleep 10"})
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if _, err := runMacOSHelper(ctx, "probe"); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("unexpected acknowledgement", func(t *testing.T) {
		requireMacOS(t)
		fakeBackendCommands(t, map[string]string{macOSHelperName: "echo unavailable"})
		if err := (MacOSBackend{}).Probe(context.Background()); err == nil {
			t.Fatal("accepted invalid readiness response")
		}
		if err := (MacOSBackend{}).SetWallpaper(context.Background(), "/image.png", DisplayOptions{}); err == nil {
			t.Fatal("accepted invalid apply response")
		}
	})
	t.Run("invalid options never invoke helper", func(t *testing.T) {
		fakeBackendCommands(t, nil)
		for _, options := range []DisplayOptions{{Fit: "stretch"}, {Fit: "crop", Upscale: true}} {
			err := (MacOSBackend{}).SetWallpaper(context.Background(), "/image.png", options)
			if err == nil || errors.Is(err, exec.ErrNotFound) {
				t.Fatalf("unexpected error: %v", err)
			}
		}
	})
	t.Run("cancelled context", func(t *testing.T) {
		fakeBackendCommands(t, nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := runMacOSHelper(ctx, "probe"); !errors.Is(err, context.Canceled) {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestMacOSUnavailableOnOtherPlatforms(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("non-macOS guard")
	}
	fakeBackendCommands(t, map[string]string{macOSHelperName: "echo ready"})
	if err := (MacOSBackend{}).Probe(context.Background()); err == nil || !strings.Contains(err.Error(), "requires macOS") {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := (MacOSBackend{}).SetWallpaper(context.Background(), "/image.png", DisplayOptions{}); err == nil || !strings.Contains(err.Error(), "requires macOS") {
		t.Fatalf("unexpected error: %v", err)
	}
}
