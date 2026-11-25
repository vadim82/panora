package playback

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/vadim82/panora/internal/source"
	"github.com/vadim82/panora/internal/sources/local"
)

func awaitCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition did not become true")
}

func TestBackgroundDiscoveryLeavesPlaybackAvailableAndCloseJoinsJobs(t *testing.T) {
	service, _ := testService(t, &fakeWallpaperBackend{})
	refreshForTest(t, service)
	started, stopped := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	service.request.Source = changedProvider{listing: func(ctx context.Context, _ source.DiscoverParams) (source.Page, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-ctx.Done()
		close(stopped)
		return source.Page{}, ctx.Err()
	}}
	service.StartDiscovery()
	service.StartDiscovery()
	<-started
	if status := service.Status(); !status.Refreshing || status.Busy {
		t.Fatalf("discovery held action ownership: %+v", status)
	}
	for _, action := range []string{"resume", "pause", "next"} {
		if _, err := service.Action(t.Context(), action); err != nil {
			t.Fatalf("%s blocked behind discovery: %v", action, err)
		}
	}
	if status := service.Status(); status.HistoryLength != 1 || status.Busy {
		t.Fatalf("playback did not progress: %+v", status)
	}
	service.Close()
	select {
	case <-stopped:
	default:
		t.Fatal("Close returned before discovery stopped")
	}
	if calls.Load() != 1 {
		t.Fatalf("duplicate discovery coordinators: %d", calls.Load())
	}
}

func TestColdCatalogReportsPendingAndRotationStartsAfterDiscovery(t *testing.T) {
	service, _ := testService(t, &fakeWallpaperBackend{})
	started, release := make(chan struct{}), make(chan struct{})
	service.request.Source = changedProvider{listing: func(ctx context.Context, p source.DiscoverParams) (source.Page, error) {
		close(started)
		select {
		case <-release:
			return (local.Provider{}).Discover(ctx, p)
		case <-ctx.Done():
			return source.Page{}, ctx.Err()
		}
	}}
	service.StartDiscovery()
	<-started
	status, err := service.Action(t.Context(), "next")
	typed, ok := err.(*Error)
	if !ok || typed.Code != "discovery_pending" || status.HistoryLength != 0 || status.Uncertain {
		t.Fatalf("cold catalog was not pending: %+v %v", status, err)
	}
	if _, err := service.Action(t.Context(), "resume"); err != nil {
		t.Fatal(err)
	}
	// Force an initial automatic attempt, matching an unpaused first startup.
	service.actionMu.Lock()
	service.nextAt = time.Now()
	service.publishStatus()
	service.actionMu.Unlock()
	done := make(chan struct{})
	go func() { defer close(done); service.Run(nil) }()
	awaitCondition(t, func() bool {
		status := service.Status()
		return status.LastError != nil && status.LastError.Code == "discovery_pending"
	})
	close(release)
	defer func() {
		if t.Failed() {
			t.Logf("final playback status: %+v", service.Status())
		}
	}()
	awaitCondition(t, func() bool { return service.Status().HistoryLength == 1 })
	service.Close()
	<-done
}

func TestChangedInputRejectsInFlightDiscoveryPage(t *testing.T) {
	service, root := testService(t, &fakeWallpaperBackend{})
	config, _, err := LoadConfig(service.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	service.request.Source = changedProvider{listing: func(ctx context.Context, p source.DiscoverParams) (source.Page, error) {
		page, err := (local.Provider{}).Discover(ctx, p)
		close(started)
		<-release
		return page, err
	}}
	result := make(chan []string, 1)
	go func() { result <- service.refreshCandidates(t.Context(), config) }()
	<-started
	data, err := os.ReadFile(service.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	nextRoot := t.TempDir()
	data = []byte(strings.Replace(string(data), root, nextRoot, 1))
	if err := os.WriteFile(service.request.ConfigPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	close(release)
	if warnings := <-result; len(warnings) == 0 {
		t.Fatal("stale page was accepted")
	}
	p := config.Providers[0]
	candidates, err := service.store.Candidates(t.Context(), p.ID, p.Folders[0].ID, config.InputRevision(p.ID, p.Folders[0].ID), maxCatalogCandidates)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("stale input enrolled candidates: %+v %v", candidates, err)
	}
}

func TestLocalDiscoveryCommitsWhenHeaderBudgetIsUnavailable(t *testing.T) {
	s, _ := testService(t, &fakeWallpaperBackend{})
	config, _, err := LoadConfig(s.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	provider := config.Providers[0]
	input := providerInputs(provider, config.Filters.MaxDepth, sourceDefaultRules(config))[0]
	ctx, cancel := context.WithTimeout(t.Context(), 450*time.Millisecond)
	defer cancel()
	counts, err := s.refreshInputCounts(ctx, provider, input, 1)
	if err != nil || counts.Pages != 1 || counts.Inspected != 3 {
		t.Fatalf("discovery did not commit the page: %+v %v", counts, err)
	}
	rows, err := s.store.Candidates(t.Context(), provider.ID, input.id, input.revision, 10)
	if err != nil || len(rows) != 3 || rows[0].Candidate.Dimensions != nil {
		t.Fatalf("candidates should survive with unknown dimensions: %v %v", rows, err)
	}
}

// Explicit fixture discovery uses the same per-input operation as the coordinator.
func (s *Service) refreshCandidates(ctx context.Context, config Config) []string {
	warnings := []string{}
	for _, p := range config.Providers {
		if !p.Enabled {
			continue
		}
		for _, input := range providerInputs(p, config.Filters.MaxDepth, sourceDefaultRules(config)) {
			if input.disabled {
				continue
			}
			if _, err := s.refreshInputCounts(ctx, p, input, maxDiscoveryPages); err != nil {
				warnings = append(warnings, err.Error())
			}
		}
	}
	return warnings
}

func TestRefreshSchedulesWithControlledTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, root := testService(t, &fakeWallpaperBackend{})
		body := localTOML(root) + "refresh_interval='10s'\n"
		if err := os.WriteFile(s.request.ConfigPath, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		var calls atomic.Int32
		s.request.Source = changedProvider{listing: func(ctx context.Context, p source.DiscoverParams) (source.Page, error) {
			calls.Add(1)
			return source.Page{Candidates: []source.Candidate{}, NextCursor: json.RawMessage("null")}, nil
		}}
		s.StartDiscovery()
		synctest.Wait()
		if calls.Load() != 1 || !s.Status().Paused {
			t.Fatalf("startup while paused: calls=%d", calls.Load())
		}
		time.Sleep(9 * time.Second)
		synctest.Wait()
		if calls.Load() != 1 {
			t.Fatal("early refresh")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if calls.Load() != 2 {
			t.Fatalf("scheduled calls=%d", calls.Load())
		}
		// Shorten the input schedule without changing its discovery revision.
		body = strings.Replace(body, "refresh_interval='10s'", "refresh_interval='2s'", 1)
		if err := os.WriteFile(s.request.ConfigPath, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if calls.Load() != 3 {
			t.Fatalf("reconciled calls=%d", calls.Load())
		}
		s.Close()
		restarted := openTestService(t, s.request, s.resolve, time.Hour, true)
		restarted.StartDiscovery()
		synctest.Wait()
		if calls.Load() != 4 {
			t.Fatalf("restart did not refresh immediately: %d", calls.Load())
		}
	})
}

func TestReconcileDiscoveryCoalescesDeadlinesAndCancelsObsoleteJobs(t *testing.T) {
	config, _, err := LoadConfig(writeConfigBody(t, localTOML(t.TempDir())))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	jobs := map[string]*discoverySchedule{}
	reconcileDiscovery(jobs, config, now)
	first := jobs["local/home"]
	first.completed = now
	first.next = now.Add(first.input.interval)
	reconcileDiscovery(jobs, config, now.Add(24*time.Hour))
	if len(jobs) != 1 || jobs["local/home"] != first || !first.next.Equal(now.Add(5*time.Minute)) {
		t.Fatal("missed intervals created extra jobs or reset deadline")
	}
	ctx, cancel := context.WithCancel(t.Context())
	first.cancel = cancel
	config.Filters.MaxDepth++
	reconcileDiscovery(jobs, config, now)
	if ctx.Err() == nil || jobs["local/home"] == first {
		t.Fatal("obsolete revision not canceled/replaced")
	}
	ctx, cancel = context.WithCancel(t.Context())
	jobs["local/home"].cancel = cancel
	config.Providers[0].Enabled = false
	reconcileDiscovery(jobs, config, now)
	if ctx.Err() == nil || len(jobs) != 0 {
		t.Fatal("disabled input retained")
	}
}

func TestDiscoveryCapacityAndCoalescingWithControlledTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, root := testService(t, &fakeWallpaperBackend{})
		body := localTOML(root) + "refresh_interval='1s'\n"
		for i := 0; i < 8; i++ {
			body += fmt.Sprintf("[[providers.folders]]\nid='folder%d'\npath=%q\nrefresh_interval='1s'\n", i, root)
		}
		body += "[[providers]]\nid='wallhaven'\nenabled=true\n[[providers.searches]]\nid='remote'\nquery='nature'\n"
		if err := os.WriteFile(s.request.ConfigPath, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		var localCalls, active, maximum, remoteCalls atomic.Int32
		s.request.Source = changedProvider{listing: func(ctx context.Context, _ source.DiscoverParams) (source.Page, error) {
			localCalls.Add(1)
			n := active.Add(1)
			defer active.Add(-1)
			for old := maximum.Load(); n > old && !maximum.CompareAndSwap(old, n); old = maximum.Load() {
			}
			<-ctx.Done()
			return source.Page{}, ctx.Err()
		}}
		s.request.Providers = map[string]source.Provider{"wallhaven": metadataProvider{changedProvider{listing: func(context.Context, source.DiscoverParams) (source.Page, error) {
			remoteCalls.Add(1)
			return source.Page{Candidates: []source.Candidate{}, NextCursor: json.RawMessage("null")}, nil
		}}}}
		s.StartDiscovery()
		synctest.Wait()
		if remoteCalls.Load() != 1 || localCalls.Load() != 1 {
			t.Fatal("independent remote/local dispatch failed")
		}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if localCalls.Load() != 1 {
			t.Fatal("duplicate job while input active")
		}
		time.Sleep(28 * time.Second)
		synctest.Wait()
		if maximum.Load() != 1 || localCalls.Load() > 11 {
			t.Fatalf("unbounded or catch-up jobs: max=%d calls=%d", maximum.Load(), localCalls.Load())
		}
		s.Close()
		if active.Load() != 0 {
			t.Fatal("shutdown did not join workers")
		}
	})
}
