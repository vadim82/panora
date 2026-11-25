package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/desktop"
)

func TestNoisyDesktopFailureLeavesSocketStatusReadable(t *testing.T) {
	options, socket := serviceFixture(t)
	// This exceeds the control client's 1 MiB response limit if the setter
	// retains the complete output in the service's last application error.
	script := "i=0; while [ \"$i\" -lt 2048 ]; do printf '" + strings.Repeat("x", 1024) + "' >&2; i=$((i+1)); done\nexit 7\n"
	fakeBackendCommands(t, map[string]string{"noctalia": script})
	launchService(t, options, socket, func(context.Context) (desktop.WallpaperBackend, error) {
		return desktop.NoctaliaBackend{}, nil
	})
	awaitDiscovery(t, socket)
	_, err := control.Request(t.Context(), socket, "next")
	var typed *control.Error
	if !errors.As(err, &typed) || typed.Code != "apply_uncertain" || !strings.Contains(typed.Message, "output limit exceeded") {
		t.Fatalf("expected a readable uncertain application failure, got %v", err)
	}
	status, err := control.Request(t.Context(), socket, "")
	if err != nil {
		t.Fatalf("status became unreadable after noisy desktop failure: %v", err)
	}
	if !status.Paused || !status.Uncertain || status.LastError == nil || status.LastError.Code != "apply_uncertain" {
		t.Fatalf("status did not retain the paused uncertain outcome: %+v", status)
	}
	if status.Current != nil || status.HistoryLength != 0 {
		t.Fatalf("failed setter was recorded as an accepted image: %+v", status)
	}
}
