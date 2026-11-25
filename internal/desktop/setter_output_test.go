package desktop

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWallpaperSettersBoundCommandOutput(t *testing.T) {
	for _, backend := range []struct {
		name    string
		command string
		setter  WallpaperBackend
		options DisplayOptions
	}{
		{name: "omarchy", command: "omarchy", setter: OmarchyBackend{}, options: DisplayOptions{Fit: "crop"}},
		{name: "noctalia", command: "noctalia", setter: NoctaliaBackend{}},
	} {
		for _, stream := range []string{"stdout", "stderr"} {
			t.Run(backend.name+"/"+stream, func(t *testing.T) {
				redirect := ""
				if stream == "stderr" {
					redirect = " >&2"
				}
				// The finite burst keeps a regressed test's memory use bounded.
				// Without an output cap the command waits for the caller deadline.
				script := "i=0; while [ \"$i\" -lt 2048 ]; do printf '" + strings.Repeat("x", 1024) + "'" + redirect + "; i=$((i+1)); done\nexec /bin/sleep 10\n"
				fakeBackendCommands(t, map[string]string{backend.command: script})
				ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
				defer cancel()
				err := backend.setter.SetWallpaper(ctx, "/fixture.png", backend.options)
				if err == nil || !strings.Contains(err.Error(), "output limit exceeded") {
					t.Fatalf("expected output overflow failure, got %v", err)
				}
				if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("setter waited for the full caller deadline: %v", err)
				}
				if size := len(err.Error()); size >= 512<<10 {
					t.Fatalf("setter retained an oversized diagnostic: %d bytes", size)
				}
			})
		}
	}
}
