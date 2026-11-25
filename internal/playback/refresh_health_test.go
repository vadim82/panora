package playback

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/vadim82/panora/internal/source"
)

func TestRefreshHealthEmptyFailureAndRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, root := testService(t, &fakeWallpaperBackend{})
		body := localTOML(root) + "refresh_interval='2s'\n"
		if err := os.WriteFile(s.request.ConfigPath, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		var calls atomic.Int32
		s.request.Source = changedProvider{listing: func(context.Context, source.DiscoverParams) (source.Page, error) {
			if calls.Add(1) == 2 {
				return source.Page{}, source.Failure("unavailable", "SECRET raw plugin traffic")
			}
			return source.Page{Candidates: []source.Candidate{}, NextCursor: json.RawMessage("null")}, nil
		}}
		s.StartDiscovery()
		synctest.Wait()
		status := s.Status()
		health := status.Providers[0].Inputs[0]
		if status.RefreshHealthVersion != 1 || health.State != "empty" || health.LastAttempt == nil || health.LastSuccess == nil || health.Counts == nil || health.Counts.Inspected != 0 || health.Counts.Pages != 1 || health.Counts.KnownEligible != nil {
			t.Fatalf("empty success: %+v", health)
		}
		lastSuccess := *health.LastSuccess
		time.Sleep(2 * time.Second)
		synctest.Wait()
		status = s.Status()
		health = status.Providers[0].Inputs[0]
		if health.State != "failed" || health.Failure.Code != "unavailable" || !health.LastSuccess.Equal(lastSuccess) || status.Providers[0].State != "failed" {
			t.Fatalf("failure: %+v", health)
		}
		raw, _ := json.Marshal(status)
		if strings.Contains(string(raw), "SECRET") {
			t.Fatal("provider text leaked into status")
		}
		time.Sleep(5 * time.Second)
		synctest.Wait()
		status = s.Status()
		health = status.Providers[0].Inputs[0]
		if health.State != "empty" || health.Failure != nil || len(status.RefreshWarnings) != 0 || !health.LastSuccess.After(lastSuccess) {
			t.Fatalf("recovery: %+v", status)
		}
		// Every pointer and slice returned to a caller belongs to that snapshot.
		health.Counts.Inspected = 900
		*health.LastSuccess = time.Time{}
		status.Providers[0].Inputs[0].ID = "mutated"
		fresh := s.Status().Providers[0].Inputs[0]
		if fresh.ID == "mutated" || fresh.LastSuccess.IsZero() || fresh.Counts.Inspected != 0 {
			t.Fatal("mutable status escaped")
		}
		s.Close()
		restarted := openTestService(t, s.request, s.resolve, time.Hour, true)
		if len(restarted.Status().Providers) != 0 {
			t.Fatal("process-local health survived restart")
		}
	})
}

func TestRefreshHealthRejectsLateRevisionAndRemainsReadable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, root := testService(t, &fakeWallpaperBackend{})
		started, release := make(chan struct{}), make(chan struct{})
		var calls atomic.Int32
		s.request.Source = changedProvider{listing: func(ctx context.Context, _ source.DiscoverParams) (source.Page, error) {
			if calls.Add(1) == 1 {
				close(started)
				<-release
				return source.Page{}, errors.New("obsolete failure")
			}
			return source.Page{Candidates: []source.Candidate{}, NextCursor: json.RawMessage("null")}, nil
		}}
		s.StartDiscovery()
		<-started
		synctest.Wait()
		old := s.Status().Providers[0].Inputs[0]
		if old.State != "refreshing" || old.LastAttempt == nil || old.Counts != nil {
			t.Fatalf("active health: %+v", old)
		}
		body := strings.Replace(localTOML(root), root, t.TempDir(), 1)
		if err := os.WriteFile(s.request.ConfigPath, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Second)
		synctest.Wait()
		current := s.Status().Providers[0].Inputs[0]
		if current.Revision == old.Revision || current.State != "pending" {
			t.Fatalf("obsolete status retained: %+v", current)
		}
		close(release)
		synctest.Wait()
		current = s.Status().Providers[0].Inputs[0]
		if current.State != "empty" || current.Failure != nil || current.Revision == old.Revision {
			t.Fatalf("late result overwrote health: %+v", current)
		}
		s.Close()
	})
}

func TestRefreshHealthCountsScopeAndBounds(t *testing.T) {
	s, root := testService(t, &fakeWallpaperBackend{})
	config, _, _ := LoadConfig(s.request.ConfigPath)
	input := providerInputs(config.Providers[0], config.Filters.MaxDepth, sourceDefaultRules(config))[0]
	counts, err := s.refreshInputCounts(t.Context(), config.Providers[0], input, 1000)
	if err != nil || counts.Inspected != 3 || counts.KnownEligible != nil || counts.Scope != "committed_metadata_in_attempt" {
		t.Fatalf("counts=%+v err=%v", counts, err)
	}
	for i := 0; i < maxHealthInputs+5; i++ {
		config.Providers[0].Folders = append(config.Providers[0].Folders, FolderConfig{ID: strings.Repeat("x", 128), Path: root})
	}
	jobs := map[string]*discoverySchedule{}
	reconcileDiscovery(jobs, config, time.Now())
	s.publishDiscoveryHealth(config, jobs, false, "")
	status := s.Status()
	p := status.Providers[0]
	if len(p.Inputs) != maxHealthInputs || p.TotalInputs != maxHealthInputs+6 || p.OmittedInputs != 6 {
		t.Fatalf("unbounded health: total=%d returned=%d omitted=%d", p.TotalInputs, len(p.Inputs), p.OmittedInputs)
	}
	raw, err := json.Marshal(status)
	if err != nil || len(raw) > 1<<20 {
		t.Fatalf("status exceeds client byte budget: %d %v", len(raw), err)
	}
}
