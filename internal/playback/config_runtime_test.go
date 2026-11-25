package playback

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/vadim82/panora/internal/filelock"
)

func TestIntervalEditWakesExistingSchedulerAtFreshDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		backend := &firstRunBackend{}
		service, _ := testService(t, backend)
		refreshForTest(t, service)
		act(t, service, "next")
		act(t, service, "resume")
		done := make(chan struct{})
		go func() { service.Run(nil); close(done) }()
		defer func() { service.Close(); <-done }()
		synctest.Wait()
		time.Sleep(10 * time.Minute)
		savedAt := time.Now()
		before := settings(t, service)
		updated, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, Interval: configValue("5m")})
		if err != nil || updated.Effective.Interval != "5m0s" || slices.Contains(updated.RestartRequired, "interval") {
			t.Fatalf("saved=%+v %v", updated, err)
		}
		status := service.Status()
		if status.Interval != "5m0s" || status.NextAt == nil || !status.NextAt.Equal(savedAt.Add(5*time.Minute)) || status.Paused || backend.calls.Load() != 1 {
			t.Fatalf("deadline=%+v calls=%d", status, backend.calls.Load())
		}
		synctest.Wait()
		time.Sleep(4*time.Minute + 59*time.Second)
		synctest.Wait()
		if backend.calls.Load() != 1 {
			t.Fatal("interval edit applied a wallpaper early")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if backend.calls.Load() != 2 || service.Status().HistoryLength != 2 {
			t.Fatalf("scheduler did not use changed interval=%+v calls=%d", service.Status(), backend.calls.Load())
		}
		// The next accepted change starts exactly one new interval; no old timer runs.
		time.Sleep(5 * time.Minute)
		synctest.Wait()
		if backend.calls.Load() != 3 {
			t.Fatalf("duplicate or missing scheduler: calls=%d", backend.calls.Load())
		}
	})
}

func TestIntervalEditWhilePausedAndManualNavigationUseNewInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		backend := &firstRunBackend{}
		service, _ := testService(t, backend)
		act(t, service, "next")
		first := service.Status().Current.ID
		before := settings(t, service)
		updated, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, Interval: configValue("2m")})
		if err != nil || !service.Status().Paused || service.Status().NextAt != nil || backend.calls.Load() != 1 {
			t.Fatalf("paused edit=%+v %v", updated, err)
		}
		time.Sleep(time.Minute)
		act(t, service, "next")
		time.Sleep(time.Minute)
		act(t, service, "previous")
		if service.Status().Current.ID != first || !service.Status().Paused || backend.calls.Load() != 3 {
			t.Fatal("manual navigation changed pause or history")
		}
		resumedAt := time.Now()
		act(t, service, "resume")
		if status := service.Status(); status.NextAt == nil || !status.NextAt.Equal(resumedAt.Add(2*time.Minute)) {
			t.Fatalf("resume deadline=%+v", status)
		}
		time.Sleep(time.Second)
		movedAt := time.Now()
		act(t, service, "next")
		if status := service.Status(); status.NextAt == nil || !status.NextAt.Equal(movedAt.Add(2*time.Minute)) {
			t.Fatalf("manual next deadline=%+v", status)
		}
		service.Close()
	})
}

func TestExplicitNoOpIntervalReplacesStartupOverrideWithoutRewritingTOML(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		service, _, _ := configurationFixture(t, "# preserve exact bytes\nversion=1\n[playback]\ninterval='30m'\n")
		before := settings(t, service)
		unrelated, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, MinWidth: configValue(1920)})
		if err != nil || unrelated.Effective.Interval != "1h0m0s" {
			t.Fatalf("unrelated save replaced override=%+v %v", unrelated, err)
		}
		deadline := time.Now().Add(30 * time.Minute)
		updated, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: unrelated.Revision, Interval: configValue("30m")})
		if err != nil || updated.Revision != unrelated.Revision || updated.TOML != unrelated.TOML || updated.Effective.Interval != "30m0s" || !service.nextAt.Equal(deadline) {
			t.Fatalf("explicit no-op=%+v %v", updated, err)
		}
		time.Sleep(time.Minute)
		// A semantically equal duration can alter saved text without restarting time.
		updated, err = service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: updated.Revision, Interval: configValue("1800s")})
		if err != nil || !service.nextAt.Equal(deadline) || updated.Effective.Interval != "30m0s" {
			t.Fatalf("equal duration reset deadline=%+v %v", updated, err)
		}
		service.Close()
	})
}

func TestIntervalFailuresAndConcurrentActionsPreserveActiveDeadline(t *testing.T) {
	service, _, backend := configurationFixture(t, "version=1\n")
	before := settings(t, service)
	next := service.nextAt
	for _, request := range []UpdateConfigurationRequest{
		{Revision: before.Revision, Interval: configValue("0s")},
		{Revision: before.Revision, Interval: configValue("5m"), MinWidth: configValue(0)},
		{Revision: "stale", Interval: configValue("5m")},
	} {
		if _, err := service.UpdateConfiguration(t.Context(), request); err == nil {
			t.Fatal("invalid interval save succeeded")
		}
		if service.interval != time.Hour || !service.nextAt.Equal(next) || settings(t, service).Revision != before.Revision || backend.calls.Load() != 0 {
			t.Fatal("rejected save changed interval or deadline")
		}
	}
	service.actionMu.Lock()
	_, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, Interval: configValue("5m")})
	requireConfigError(t, err, "busy")
	service.actionMu.Unlock()
	if service.interval != time.Hour || !service.nextAt.Equal(next) {
		t.Fatal("busy save changed active timing")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = service.UpdateConfiguration(ctx, UpdateConfigurationRequest{Revision: before.Revision, Interval: configValue("5m")})
	if err != context.Canceled || service.interval != time.Hour || !service.nextAt.Equal(next) {
		t.Fatalf("canceled edit=%v", err)
	}
}

func TestIntervalRestartUsesSavedPreferenceAndPersistedLastAction(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		service, _ := testService(t, &firstRunBackend{})
		act(t, service, "next")
		lastAction := service.state.LastActionAt
		time.Sleep(10 * time.Minute)
		before := settings(t, service)
		if _, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, Interval: configValue("5m")}); err != nil {
			t.Fatal(err)
		}
		if service.state.LastActionAt != lastAction || !service.nextAt.Equal(time.Now().Add(5*time.Minute)) {
			t.Fatal("interval edit rewrote playback state or failed fresh session deadline")
		}
		service.Close()
		restarted, err := OpenService(t.Context(), ServiceOptions{Request: service.request, Backend: "fake"}, service.resolve)
		if err != nil {
			t.Fatal(err)
		}
		if restarted.interval != 5*time.Minute || !restarted.nextAt.Equal(lastAction.Add(5*time.Minute)) || !restarted.nextAt.Before(time.Now()) {
			t.Fatalf("saved restart timing=%+v", restarted.Status())
		}
		restarted.Close()
		override, err := OpenService(t.Context(), ServiceOptions{Request: service.request, Interval: time.Hour, IntervalChanged: true, Backend: "fake"}, service.resolve)
		if err != nil {
			t.Fatal(err)
		}
		defer override.Close()
		if got := settings(t, override); got.Saved.Interval != "5m" || got.Effective.Interval != "1h0m0s" || !slices.Contains(got.RestartRequired, "interval") {
			t.Fatalf("restart override=%+v", got)
		}
	})
}

func TestIntervalWriteFailureLeavesActiveDeadlineUntouched(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	service, path, _ := configurationFixture(t, "version=1\n")
	before := settings(t, service)
	next := service.nextAt
	if err := os.Chmod(filepath.Dir(path), 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(path), 0700) })
	_, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, Interval: configValue("5m")})
	requireConfigError(t, err, "config_write_failed")
	if service.interval != time.Hour || !service.nextAt.Equal(next) || settings(t, service).Revision != before.Revision {
		t.Fatal("failed publication changed active timing")
	}
}

func TestIntervalSnapshotsRaceWithSavedEdits(t *testing.T) {
	service, _, _ := configurationFixture(t, "version=1\n")
	var readers sync.WaitGroup
	stop := make(chan struct{})
	for range 4 {
		readers.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					_, err := service.Configuration(t.Context())
					if err != nil {
						t.Errorf("read settings=%v", err)
						return
					}
					_ = service.Status()
				}
			}
		})
	}
	defer func() { close(stop); readers.Wait() }()
	for _, value := range []string{"1m", "2m", "3m", "4m", "5m"} {
		before := settings(t, service)
		updated, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, Interval: &value})
		if err != nil || updated.Saved.Interval != value {
			t.Fatalf("save=%+v %v", updated, err)
		}
	}
}

// This fixture intentionally holds playback ownership past an old deadline,
// matching a blocked action/suspend-like delay. Releasing it schedules one change.
func TestIntervalChangesCoalesceOverdueSchedulerWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		backend := &firstRunBackend{}
		service, _ := testService(t, backend)
		act(t, service, "next")
		act(t, service, "resume")
		before := settings(t, service)
		if _, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, Interval: configValue("1m")}); err != nil {
			t.Fatal(err)
		}
		service.actionMu.Lock()
		locked := true
		done := make(chan struct{})
		go func() { service.Run(nil); close(done) }()
		defer func() {
			if locked {
				service.actionMu.Unlock()
			}
			service.Close()
			<-done
		}()
		time.Sleep(10 * time.Minute)
		synctest.Wait()
		if backend.calls.Load() != 1 {
			t.Fatal("scheduler applied while playback was owned")
		}
		service.actionMu.Unlock()
		locked = false
		service.notify()
		synctest.Wait()
		if backend.calls.Load() != 2 || service.Status().HistoryLength != 2 {
			t.Fatalf("missed deadlines replayed in a burst: calls=%d", backend.calls.Load())
		}
	})
}

func TestCanceledNoOpIntervalEditDoesNotApplyAfterWaiting(t *testing.T) {
	service, path, _ := configurationFixture(t, "version=1\n")
	before := settings(t, service)
	next := service.nextAt
	service.configMu.Lock()
	locked := true
	defer func() {
		if locked {
			service.configMu.Unlock()
		}
	}()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := service.UpdateConfiguration(ctx, UpdateConfigurationRequest{Revision: before.Revision, Interval: configValue(before.Saved.Interval)})
		done <- err
	}()
	// Contention proves the writer already acquired the file lock and cannot
	// return early from that wait when cancellation arrives.
	awaitCondition(t, func() bool {
		release, err := filelock.Try(path + ".lock")
		if err == nil {
			release()
			return false
		}
		if !errors.Is(err, filelock.ErrBusy) {
			t.Fatal(err)
		}
		return true
	})
	cancel()
	service.configMu.Unlock()
	locked = false
	if err := <-done; err != context.Canceled {
		t.Fatalf("canceled no-op=%v", err)
	}
	if service.interval != time.Hour || !service.nextAt.Equal(next) || settings(t, service).Revision != before.Revision {
		t.Fatal("canceled no-op applied the saved interval")
	}
}

func TestRunningSchedulerAndIntervalSavesShareMutationOwnership(t *testing.T) {
	backend := &firstRunBackend{}
	service, _ := testService(t, backend)
	refreshForTest(t, service)
	act(t, service, "next")
	act(t, service, "resume")
	done := make(chan struct{})
	go func() { service.Run(nil); close(done) }()
	defer func() { service.Close(); <-done }()
	successes := 0
	for attempts := 0; attempts < 200 && successes < 20; attempts++ {
		before := settings(t, service)
		value := "1ms"
		if successes%2 == 0 {
			value = "2ms"
		}
		_, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, Interval: &value})
		if err == nil {
			successes++
			continue
		}
		requireConfigError(t, err, "busy")
		time.Sleep(time.Millisecond)
	}
	if successes < 20 {
		t.Fatalf("saved only %d interval changes", successes)
	}
	if !slices.Contains([]string{"1ms", "2ms"}, service.Status().Interval) {
		t.Fatalf("active interval=%s", service.Status().Interval)
	}
}
