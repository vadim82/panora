package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const oneDisplay = `[{"name":"DP-2","width":5120,"height":1440,"scale":1,"transform":0}]`

func capabilityCommands(t *testing.T, monitors string, noctalia bool) {
	t.Helper()
	shell := "printf 'ok\\n'"
	noctaliaScript := "exit 1"
	if noctalia {
		noctaliaScript = `case "$*" in
 'msg wallpaper-get') printf '/test/current.png\n';;
 'config export full') printf '[wallpaper]\nenabled=true\n';;
 *) exit 9;;
 esac`
	}
	fakeBackendCommands(t, map[string]string{
		"omarchy":       "exit 9", // Readiness may locate this command, never run its setter.
		"omarchy-shell": shell,
		"qs":            "printf 'target background\\nfunction set(path: string): void\\n'",
		"hyprctl":       "[ \"$*\" = 'monitors -j' ] || exit 9\nprintf '%s' '" + monitors + "'",
		"noctalia":      noctaliaScript,
	})
}

func TestCapabilityInspectionOmarchyPresentationsAndGeometry(t *testing.T) {
	for _, tc := range []struct {
		name, monitors, state, geometry string
		contain                         bool
		displays                        int
	}{
		{"single", oneDisplay, "ready", "known", true, 1},
		{"disabled ignored", `[{"disabled":true},` + oneDisplay[1:], "ready", "known", true, 1},
		{"multiple", `[{"name":"A","width":1920,"height":1080,"scale":1},{"name":"B","width":2560,"height":1440,"scale":1}]`, "partial", "ambiguous", false, 2},
		{"rotated", `[{"name":"DP-2","width":1440,"height":2560,"scale":1,"transform":1}]`, "partial", "known", false, 1},
		{"no outputs", `[]`, "partial", "unavailable", false, 0},
		{"malformed", `broken`, "partial", "unavailable", false, 0},
		{"invalid scale", `[{"name":"DP-2","width":1920,"height":1080,"scale":0}]`, "partial", "unavailable", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capabilityCommands(t, tc.monitors, false)
			got, err := InspectCapabilities(t.Context(), "omarchy")
			if err != nil || got.Version != 1 || got.Backend != "omarchy" || got.State != tc.state || got.Geometry.State != tc.geometry || len(got.Geometry.Displays) != tc.displays || got.AssignmentScope != "all_monitors" || got.IndependentOutputs {
				t.Fatalf("capabilities=%+v err=%v", got, err)
			}
			if len(got.Backends) != 1 || !got.Backends[0].Ready || len(got.Presentations) != 3 || got.Presentations[0].Available != tc.contain || got.Presentations[1].Upscale != tc.contain || !got.Presentations[2].Available || got.Presentations[2].Upscale {
				t.Fatalf("readiness/presentation=%+v", got)
			}
		})
	}
}

func TestCapabilitiesAutoAndExplicitReadiness(t *testing.T) {
	capabilityCommands(t, oneDisplay, true)
	got, err := InspectCapabilities(t.Context(), "auto")
	if err != nil || got.State != "ambiguous" || got.Backend != "" || got.Geometry.State != "ambiguous" || len(got.Presentations) != 0 {
		t.Fatalf("auto=%+v %v", got, err)
	}
	got, err = InspectCapabilities(t.Context(), "noctalia")
	if err != nil || got.Backend != "noctalia" || got.State != "partial" || got.Geometry.State != "unsupported" || got.Geometry.Failure.Code != "geometry_unsupported" || !got.Presentations[0].Available || got.Presentations[0].Upscale || got.Presentations[1].Available || got.Presentations[2].Available {
		t.Fatalf("Noctalia=%+v %v", got, err)
	}
}

func TestCapabilityFailuresRetainCauseWithoutExposingOutput(t *testing.T) {
	fakeBackendCommands(t, map[string]string{"omarchy": "exit 9", "omarchy-shell": "printf 'SECRET' >&2\nexit 7"})
	got, err := InspectCapabilities(t.Context(), "omarchy")
	if err != nil || got.State != "unavailable" || len(got.Backends) != 1 || got.Backends[0].Ready || got.Backends[0].Failure.Code != "backend_unavailable" {
		t.Fatalf("unavailable=%+v %v", got, err)
	}
	var exit *exec.ExitError
	if !errors.As(got.Backends[0].Failure, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("typed cause lost=%v", got.Backends[0].Failure)
	}
	encoded, err := json.Marshal(got)
	if err != nil || strings.Contains(string(encoded), "SECRET") {
		t.Fatalf("raw error escaped=%s %v", encoded, err)
	}
}

func TestCapabilityQueriesHonorCancellationAndBounds(t *testing.T) {
	fakeBackendCommands(t, map[string]string{"omarchy": "exit 9", "omarchy-shell": "exec /bin/sleep 5"})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := InspectCapabilities(ctx, "omarchy"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline cause lost=%v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("query ignored cancellation")
	}
	ctx, cancel = context.WithCancel(t.Context())
	cancel()
	if _, err := InspectCapabilities(ctx, "auto"); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled=%v", err)
	}
}

func TestMacOSCapabilityScopeUsesBackingGeometryWithoutClaimingAllSpaces(t *testing.T) {
	got, err := inspectCapabilities(t.Context(), "macos", []Selection{{Name: "macos", Backend: &fakeWallpaperBackend{probe: func(context.Context) error { return nil }}}}, func(context.Context) (Geometry, error) { t.Fatal("unexpected Hyprland query"); return Geometry{}, nil }, func(context.Context) (Geometry, error) {
		return parseHyprlandGeometry([]byte(`[{"name":"Retina","width":3456,"height":2234,"scale":2,"transform":0}]`))
	})
	if err != nil || got.AssignmentScope != "all_connected_screens_current_spaces" || got.Geometry.State != "known" || got.Geometry.Displays[0].Width != 3456 || got.State != "ready" || !got.Presentations[1].Available || !got.Presentations[1].Upscale || got.IndependentOutputs {
		t.Fatalf("macOS=%+v %v", got, err)
	}
}

func TestHyprlandGeometryRejectsAmbiguousOrUnboundedMetadata(t *testing.T) {
	for _, data := range []string{
		`[{"name":"A","width":1,"height":1,"scale":1},{"name":"A","width":1,"height":1,"scale":1}]`,
		`[{"name":"A","width":10000,"height":10000,"scale":1}]`,
		`[{"name":"A","width":1,"height":1,"scale":1,"transform":8}]`,
	} {
		if _, err := parseHyprlandGeometry([]byte(data)); err == nil {
			t.Fatalf("invalid geometry accepted=%s", data)
		}
	}
	displays := make([]string, 33)
	for i := range displays {
		displays[i] = fmt.Sprintf(`{"name":"D%d","width":1,"height":1,"scale":1}`, i)
	}
	if _, err := parseHyprlandGeometry([]byte("[" + strings.Join(displays, ",") + "]")); err == nil || !strings.Contains(err.Error(), "exceeds 32") {
		t.Fatalf("display count not bounded=%v", err)
	}

}
