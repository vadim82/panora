package cli

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/playback"
)

func TestIntervalOverrideDoesNotSurviveRestart(t *testing.T) {
	options, _ := serviceFixture(t)
	body, err := os.ReadFile(options.Request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	body = []byte(strings.Replace(string(body), `interval = '30m'`, `interval = '45m'`, 1))
	body = []byte(strings.Replace(string(body), `interval = "30m"`, `interval = "45m"`, 1))
	if err := os.WriteFile(options.Request.ConfigPath, body, 0600); err != nil {
		t.Fatal(err)
	}
	resolve := func(context.Context) (desktop.WallpaperBackend, error) {
		t.Fatal("paused startup called desktop")
		return nil, nil
	}
	options.Interval, options.IntervalChanged = time.Hour, true
	first, err := playback.OpenService(context.Background(), options, resolve)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status().Interval != "1h0m0s" {
		t.Fatal(first.Status())
	}
	first.Close()
	options.IntervalChanged = false
	second, err := playback.OpenService(context.Background(), options, resolve)
	if err != nil {
		t.Fatal(err)
	}
	if second.Status().Interval != "45m0s" {
		t.Fatal(second.Status())
	}
	second.Close()
	updated := strings.Replace(string(body), "45m", "15m", 1)
	if err := os.WriteFile(options.Request.ConfigPath, []byte(updated), 0600); err != nil {
		t.Fatal(err)
	}
	third, err := playback.OpenService(context.Background(), options, resolve)
	if err != nil {
		t.Fatal(err)
	}
	defer third.Close()
	if third.Status().Interval != "15m0s" {
		t.Fatal(third.Status())
	}
}
