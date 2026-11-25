package desktop

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSelectWallpaperBackend(t *testing.T) {
	unavailable := errors.New("session unavailable")
	tests := []struct {
		name        string
		request     string
		omarchyErr  error
		noctaliaErr error
		want        string
		wantError   string
		wantProbes  []string
	}{
		{"only Omarchy ready", "auto", nil, unavailable, "omarchy", "", []string{"omarchy", "noctalia"}},
		{"only Noctalia ready", "auto", unavailable, nil, "noctalia", "", []string{"omarchy", "noctalia"}},
		{"both ready", "auto", nil, nil, "", "multiple wallpaper backends", []string{"omarchy", "noctalia"}},
		{"neither ready", "auto", unavailable, unavailable, "", "no wallpaper backend", []string{"omarchy", "noctalia"}},
		{"explicit Omarchy", "omarchy", nil, nil, "omarchy", "", []string{"omarchy"}},
		{"explicit Noctalia", "noctalia", nil, nil, "noctalia", "", []string{"noctalia"}},
		{"override cannot bypass readiness", "omarchy", unavailable, nil, "", "omarchy backend is not ready", []string{"omarchy"}},
		{"unknown backend", "other", nil, nil, "", "unknown backend", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var probes []string
			options := []Selection{
				{Name: "omarchy", Backend: &fakeWallpaperBackend{probe: func(context.Context) error {
					probes = append(probes, "omarchy")
					return tt.omarchyErr
				}}},
				{Name: "noctalia", Backend: &fakeWallpaperBackend{probe: func(context.Context) error {
					probes = append(probes, "noctalia")
					return tt.noctaliaErr
				}}},
			}
			selected, err := selectWallpaperBackend(context.Background(), tt.request, options)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error = %v, want %q", err, tt.wantError)
				}
				if tt.omarchyErr != nil && !errors.Is(err, unavailable) {
					t.Fatalf("probe error was lost: %v", err)
				}
			} else if err != nil || selected.Name != tt.want || selected.Backend == nil {
				t.Fatalf("selected = %+v, error = %v, want %s", selected, err, tt.want)
			}
			if !reflect.DeepEqual(probes, tt.wantProbes) {
				t.Fatalf("probes = %v, want %v", probes, tt.wantProbes)
			}
		})
	}
}

func TestSelectWallpaperBackendCancelled(t *testing.T) {
	for _, name := range []string{"auto", "omarchy"} {
		t.Run(name, func(t *testing.T) {
			t.Run("before selection", func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				options := []Selection{{Name: "omarchy", Backend: &fakeWallpaperBackend{probe: func(context.Context) error {
					t.Fatal("probed a backend after cancellation")
					return nil
				}}}}
				if _, err := selectWallpaperBackend(ctx, name, options); !errors.Is(err, context.Canceled) {
					t.Fatalf("got %v, want cancellation", err)
				}
			})
			t.Run("during probe", func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				options := []Selection{
					{Name: "omarchy", Backend: &fakeWallpaperBackend{probe: func(context.Context) error {
						cancel()
						return nil
					}}},
					{Name: "noctalia", Backend: &fakeWallpaperBackend{probe: func(context.Context) error {
						t.Fatal("probed another backend after cancellation")
						return nil
					}}},
				}
				if _, err := selectWallpaperBackend(ctx, name, options); !errors.Is(err, context.Canceled) {
					t.Fatalf("got %v, want cancellation", err)
				}
			})
		})
	}
}

func TestProbeOmarchy(t *testing.T) {
	tests := []struct {
		name      string
		ping      string
		targets   string
		wantError string
	}{
		{"ready", "ok", "target background\n  function set(path: string): void\n", ""},
		{"missing background handler", "ok", "target shell\n  function set(path: string): void\n", "background IPC handler"},
		{"background has no setter", "ok", "target background\n  function refresh(): void\ntarget other\n  function set(path: string): void\n", "background IPC handler"},
		{"unexpected ping", "Not ready to accept queries yet", "target background\n  function set(path: string): void\n", "did not acknowledge"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PANORA_TEST_PING", tt.ping)
			t.Setenv("PANORA_TEST_TARGETS", tt.targets)
			fakeBackendCommands(t, map[string]string{
				"omarchy": "exit 0\n",
				"omarchy-shell": `test "$#" = 2 && test "$1" = shell && test "$2" = ping || exit 8
printf '%s\n' "$PANORA_TEST_PING"`,
				"qs": `test "$#" = 5 && test "$1" = ipc && test "$2" = -n && test "$3" = -p && test "$4" = "$OMARCHY_PATH/shell" && test "$5" = show || exit 8
printf '%s' "$PANORA_TEST_TARGETS"`,
			})
			err := (OmarchyBackend{}).Probe(context.Background())
			if tt.wantError == "" && err != nil || tt.wantError != "" && (err == nil || !strings.Contains(err.Error(), tt.wantError)) {
				t.Fatalf("error = %v, want %q", err, tt.wantError)
			}
		})
	}
	t.Run("no installed setter", func(t *testing.T) {
		fakeBackendCommands(t, nil)
		if err := (OmarchyBackend{}).Probe(context.Background()); !errors.Is(err, exec.ErrNotFound) {
			t.Fatalf("got %v, want missing executable", err)
		}
	})
	t.Run("invalid installation path", func(t *testing.T) {
		fakeBackendCommands(t, map[string]string{"omarchy": "exit 0"})
		t.Setenv("OMARCHY_PATH", "relative")
		if err := (OmarchyBackend{}).Probe(context.Background()); err == nil || !strings.Contains(err.Error(), "absolute path") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestProbeNoctalia(t *testing.T) {
	tests := []struct {
		name      string
		wallpaper string
		config    string
		wantError string
	}{
		{"ready", "/wallpaper.png", "[wallpaper]\nenabled = true\n", ""},
		{"fresh config", "", "[wallpaper]\nenabled = true\n", ""},
		{"solid color", "color:#123456", "[wallpaper]\nenabled = true\n", ""},
		{"disabled", "/wallpaper.png", "[wallpaper]\nenabled = false\n", "disabled"},
		{"missing enabled setting", "/wallpaper.png", "[wallpaper]\nfill_mode = 'fit'\n", "does not report"},
		{"malformed config", "/wallpaper.png", "[wallpaper", "decode"},
		{"IPC error at exit zero", "error: wallpaper service not initialized", "[wallpaper]\nenabled = true\n", "did not return wallpaper state"},
		{"wrong setting type", "/wallpaper.png", "[wallpaper]\nenabled = 'true'\n", "decode"},
		{"other sections ignored", "/wallpaper.png", "[shell]\nname = 'test'\n[wallpaper]\nenabled = true\n[wallpaper.automation]\nenabled = false\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PANORA_TEST_WALLPAPER", tt.wallpaper)
			t.Setenv("PANORA_TEST_CONFIG", tt.config)
			fakeBackendCommands(t, map[string]string{"noctalia": `case "$*" in
"msg wallpaper-get") printf '%s\n' "$PANORA_TEST_WALLPAPER" ;;
"config export full") echo 'unrelated warning' >&2; printf '%s' "$PANORA_TEST_CONFIG" ;;
*) exit 8 ;;
esac`})
			err := (NoctaliaBackend{}).Probe(context.Background())
			if tt.wantError == "" && err != nil || tt.wantError != "" && (err == nil || !strings.Contains(err.Error(), tt.wantError)) {
				t.Fatalf("error = %v, want %q", err, tt.wantError)
			}
		})
	}
}

func TestBackendProbeFailures(t *testing.T) {
	t.Run("nonzero exit includes stderr", func(t *testing.T) {
		fakeBackendCommands(t, map[string]string{"noctalia": "echo 'session unavailable' >&2\nexit 7\n"})
		err := (NoctaliaBackend{}).Probe(context.Background())
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 || !strings.Contains(err.Error(), "session unavailable") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		fakeBackendCommands(t, map[string]string{"noctalia": "exec /bin/sleep 10\n"})
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if err := (NoctaliaBackend{}).Probe(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("got %v, want timeout", err)
		}
	})
}

func TestReadinessProbeOutputLimits(t *testing.T) {
	for _, stream := range []string{"stdout", "stderr"} {
		t.Run(stream, func(t *testing.T) {
			script := `i=0; while [ "$i" -lt 8192 ]; do printf '0123456789abcdef'; i=$((i+1)); done`
			if stream == "stderr" {
				script = "{ " + script + "; } >&2"
			}
			fakeBackendCommands(t, map[string]string{"probe": script})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, err := runBackendCommand(ctx, "probe"); err == nil || !strings.Contains(err.Error(), "output limit exceeded") {
				t.Fatalf("unbounded %s: %v", stream, err)
			}
		})
	}
}

func TestBackendCommandFailureDoesNotEchoQueryData(t *testing.T) {
	fakeBackendCommands(t, map[string]string{"probe": "printf 'private configuration'\nprintf 'session unavailable' >&2\nexit 7\n"})
	_, err := runBackendCommand(t.Context(), "probe")
	if err == nil || !strings.Contains(err.Error(), "session unavailable") || strings.Contains(err.Error(), "private configuration") {
		t.Fatalf("command failure exposed query data or lost its diagnostic: %v", err)
	}
}
