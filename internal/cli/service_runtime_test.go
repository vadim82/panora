package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/playback"
	"github.com/vadim82/panora/internal/service"
	"github.com/vadim82/panora/internal/source"
)

func shortSocket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "panora-cli-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "control.sock")
}

func serviceFixture(t *testing.T) (playback.ServiceOptions, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	writeScanImage(t, filepath.Join(root, "a.png"), 2, 2)
	writeScanImage(t, filepath.Join(root, "b.png"), 2, 2)
	options := playback.ServiceOptions{Request: playback.Request{ConfigPath: writeNextConfig(t, root), StatePath: filepath.Join(t.TempDir(), "state.db"), CachePath: t.TempDir(), Source: source.BundledLocal()}, Interval: time.Hour, IntervalChanged: true, Paused: true, Backend: "fake"}
	return options, shortSocket(t)
}

func awaitService(t *testing.T, socket string) control.Status {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if status, err := control.Request(context.Background(), socket, ""); err == nil {
			return status
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("service did not become ready")
	return control.Status{}
}

func launchService(t *testing.T, options playback.ServiceOptions, socket string, resolve playback.Resolver) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var runErr error
	go func() {
		runErr = service.Run(ctx, options, resolve, socket, io.Discard, io.Discard, service.Hooks{})
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
			if runErr != nil {
				t.Errorf("service shutdown: %v", runErr)
			}
		case <-time.After(5 * time.Second):
			t.Error("service did not stop")
		}
	})
	awaitService(t, socket)
	return cancel
}

func cliStatus(t *testing.T, socket, action string) control.Status {
	t.Helper()
	if action == "next" {
		awaitDiscovery(t, socket)
	}
	cmd := NewRootCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{action, "--socket", socket, "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var status control.Status
	if err := json.Unmarshal(output.Bytes(), &status); err != nil || status.Version != 1 {
		t.Fatalf("invalid CLI JSON: %s %v", output.String(), err)
	}
	return status
}

func TestServiceTimerPauseAndResume(t *testing.T) {
	options, socket := serviceFixture(t)
	options.Interval = 80 * time.Millisecond
	backend := &fakeWallpaperBackend{}
	launchService(t, options, socket, func(context.Context) (desktop.WallpaperBackend, error) { return backend, nil })
	cliStatus(t, socket, "next")
	time.Sleep(120 * time.Millisecond)
	if status := cliStatus(t, socket, "status"); status.HistoryLength != 1 {
		t.Fatal("paused service rotated automatically")
	}
	resumed := cliStatus(t, socket, "resume")
	if resumed.Paused || resumed.NextAt == nil {
		t.Fatal("resume did not schedule a turn")
	}
	deadline := time.Now().Add(3 * time.Second)
	var paused control.Status
	for time.Now().Before(deadline) {
		status, err := control.Request(context.Background(), socket, "")
		if err == nil && status.HistoryLength >= 2 {
			paused, err = control.Request(context.Background(), socket, "pause")
			if err == nil {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !paused.Paused || paused.HistoryLength < 2 {
		t.Fatal("service did not rotate after resume")
	}
	time.Sleep(120 * time.Millisecond)
	if status := cliStatus(t, socket, "status"); status.HistoryLength != paused.HistoryLength {
		t.Fatal("pause failed to stop the timer")
	}
}

func TestNextRequiresServiceAndDoesNotIgnoreOverrides(t *testing.T) {
	for _, args := range [][]string{
		{"--socket", shortSocket(t), "--json"},
		{"--config", "ignored.json"},
		{"--standalone", "--socket", shortSocket(t)},
	} {
		var output bytes.Buffer
		if err := Execute(context.Background(), append([]string{"next"}, args...), &output, io.Discard); err == nil {
			t.Fatalf("accepted %v", args)
		}
		if len(args) == 3 && args[2] == "--json" {
			var response struct {
				Version int            `json:"version"`
				Error   *control.Error `json:"error"`
			}
			if err := json.Unmarshal(output.Bytes(), &response); err != nil || response.Version != 1 || response.Error == nil {
				t.Fatalf("missing JSON failure response: %s %v", output.String(), err)
			}
		}
	}
}

func TestCachedPlaybackSurvivesUnavailableFolder(t *testing.T) {
	options, socket := serviceFixture(t)
	config, _, err := playback.LoadConfig(options.Request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeWallpaperBackend{}
	launchService(t, options, socket, func(context.Context) (desktop.WallpaperBackend, error) { return backend, nil })
	first := cliStatus(t, socket, "next")
	second := cliStatus(t, socket, "next")
	if err := os.RemoveAll(config.Providers[0].Folders[0].Path); err != nil {
		t.Fatal(err)
	}
	previous := cliStatus(t, socket, "previous")
	if previous.Current.Path != first.Current.Path {
		t.Fatal("cached history blocked by missing source folder")
	}
	forward := cliStatus(t, socket, "next")
	if forward.Current.Path != second.Current.Path {
		t.Fatal("cached forward history blocked")
	}
	fresh := cliStatus(t, socket, "next")
	if fresh.Current.Path == second.Current.Path || backend.calls != 5 {
		t.Fatalf("cached fresh selection unavailable: %+v", fresh)
	}
}

func TestManualNextRecoversRestoredLocalFolderWhilePaused(t *testing.T) {
	options, socket := serviceFixture(t)
	options.Interval = time.Second
	config, _, err := playback.LoadConfig(options.Request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	config.Providers[0].RefreshInterval = "1s"
	root := config.Providers[0].Folders[0].Path
	saved, err := toml.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(options.Request.ConfigPath, saved, 0600); err != nil {
		t.Fatal(err)
	}
	launchService(t, options, socket, func(context.Context) (desktop.WallpaperBackend, error) {
		return &fakeWallpaperBackend{}, nil
	})
	awaitHealth := func(state string) playback.InputRefreshHealth {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			status, err := control.Request(t.Context(), socket, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(status.Providers) > 0 && len(status.Providers[0].Inputs) > 0 {
				if input := status.Providers[0].Inputs[0]; input.State == state {
					return input
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("expected discovery state %s", state)
		return playback.InputRefreshHealth{}
	}
	initial := awaitHealth("ready")
	cliStatus(t, socket, "next")
	before := cliStatus(t, socket, "next")
	aside := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(root, aside); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Until(initial.LastSuccess.Add(options.Interval)) + 20*time.Millisecond)
	if idle := awaitHealth("ready"); !idle.LastAttempt.Equal(*initial.LastAttempt) {
		t.Fatal("legacy interval refreshed a paused input")
	}
	failedPlay := cliStatus(t, socket, "next")
	outage := awaitHealth("failed")
	if outage.Failure == nil || outage.Failure.Code != "unavailable" ||
		len(outage.Retries) != 1 || outage.Retries[0].Attempts != 1 || outage.NextRefresh == nil {
		t.Fatalf("expected temporary discovery failure: %+v", outage)
	}
	if err := os.Rename(aside, root); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Until(*outage.NextRefresh) + 20*time.Millisecond)
	stillFailed := awaitHealth("failed")
	if !stillFailed.LastAttempt.Equal(*outage.LastAttempt) {
		t.Fatal("idle service retried without playback demand")
	}
	after := cliStatus(t, socket, "next")
	recovered := awaitHealth("ready")
	if recovered.Failure != nil || len(recovered.Retries) != 0 || recovered.Revision != initial.Revision ||
		recovered.Counts == nil || recovered.Counts.Inspected != 2 {
		t.Fatalf("same input did not recover on Next: %+v", recovered)
	}
	if after.InstanceID != before.InstanceID || !after.Paused || after.NextAt != nil ||
		failedPlay.Current == nil || after.Current == nil || after.HistoryLength != before.HistoryLength+2 {
		t.Fatalf("cached playback did not survive discovery recovery: before %+v, after %+v", before, after)
	}
	if current, err := os.ReadFile(options.Request.ConfigPath); err != nil || !bytes.Equal(saved, current) {
		t.Fatalf("recovery changed the saved configuration: %v", err)
	}
}

func awaitDiscovery(t *testing.T, socket string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status, err := control.Request(context.Background(), socket, "")
		if err == nil && !status.Refreshing {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("discovery did not finish within its fixture budget")
}

func TestServeUsesSavedDesktopSettingsOnRestart(t *testing.T) {
	options, socket := serviceFixture(t)
	original, err := os.ReadFile(options.Request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	// Both runs read the same saved desktop preferences; CLI wiring still injects
	// a fake setter, so neither startup nor playback can reach the real desktop.
	body := strings.Replace(string(original), "backend = 'auto'", "backend = 'noctalia'", 1)
	body = strings.Replace(body, "fit = 'auto'", "fit = 'crop'", 1)
	if err := os.WriteFile(options.Request.ConfigPath, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	for run := 0; run < 2; run++ {
		backend := &fakeWallpaperBackend{}
		cmd := newServeCommand(func(context.Context) (desktop.WallpaperBackend, error) { return backend, nil })
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"--config", options.Request.ConfigPath, "--state-file", options.Request.StatePath, "--socket", socket, "--paused"})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- cmd.ExecuteContext(ctx) }()
		t.Cleanup(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("serve did not stop")
			}
		})
		status := awaitService(t, socket)
		if status.Backend != "noctalia" {
			t.Fatalf("saved backend not used: %+v", status)
		}
		awaitDiscovery(t, socket)
		if _, err := control.Request(context.Background(), socket, "next"); err != nil {
			t.Fatal(err)
		}
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("serve did not stop")
		}
		// Unblock the cleanup wait after the successful explicit join.
		close(done)
		if backend.options.Fit != "crop" {
			t.Fatalf("saved presentation not used: %+v", backend.options)
		}
	}
}
