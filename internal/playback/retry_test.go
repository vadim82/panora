package playback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/source"
)

func TestSourceFailureLogRecordsRetryWithoutProviderText(t *testing.T) {
	s, _ := testService(t, &fakeWallpaperBackend{})
	var output bytes.Buffer
	s.request.SourceLogger = slog.New(slog.NewJSONHandler(&output, nil))
	key := catalog.RetryKey{ProviderID: "wallhaven", InputID: "space", Scope: "discovery"}
	s.observeRetry(key, "discovery", context.Canceled)
	if output.Len() != 0 {
		t.Fatalf("canceled check was logged: %s", &output)
	}
	const privateProviderText = "PRIVATE_PROVIDER_RESPONSE"
	s.observeRetry(key, "discovery", source.Failure("rate_limited", privateProviderText))
	if strings.Contains(output.String(), privateProviderText) {
		t.Fatalf("source log includes raw provider text: %s", &output)
	}
	var event struct {
		Provider string    `json:"provider"`
		Input    string    `json:"input"`
		Stage    string    `json:"stage"`
		Code     string    `json:"code"`
		Attempt  int       `json:"attempt"`
		RetryAt  time.Time `json:"retry_at"`
	}
	if err := json.Unmarshal(output.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if event.Provider != "wallhaven" || event.Input != "space" || event.Stage != "discovery" ||
		event.Code != "rate_limited" || event.Attempt != 1 || !event.RetryAt.Equal(s.retryUntil(key, "provider_metadata")) {
		t.Fatalf("unexpected source failure log: %+v", event)
	}
}

func TestDiscoveryRetryRecoveryResetsBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, root := testService(t, &fakeWallpaperBackend{})
		if err := os.WriteFile(s.request.ConfigPath, []byte(localTOML(root)+"refresh_interval='1s'"), 0600); err != nil {
			t.Fatal(err)
		}
		var calls atomic.Int32
		s.request.Source = changedProvider{listing: func(context.Context, source.DiscoverParams) (source.Page, error) {
			if n := calls.Add(1); n != 3 {
				return source.Page{}, source.Failure("unavailable", "HTTP 503 fixture")
			}
			return source.Page{Candidates: []source.Candidate{}, NextCursor: json.RawMessage("null")}, nil
		}}
		s.StartDiscovery()
		synctest.Wait()
		time.Sleep(4 * time.Second)
		synctest.Wait()
		if calls.Load() != 1 {
			t.Fatal("retry before 5s")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if calls.Load() != 2 {
			t.Fatal("first backoff not applied")
		}
		time.Sleep(9 * time.Second)
		synctest.Wait()
		if calls.Load() != 2 {
			t.Fatal("second retry before 10s")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if calls.Load() != 3 || s.Status().Providers[0].Inputs[0].State != "empty" {
			t.Fatal("empty success did not recover")
		}
		if states, err := s.store.LoadRetries(t.Context()); err != nil || len(states) != 0 {
			t.Fatalf("successful attempt did not reset retries: %+v %v", states, err)
		}
		time.Sleep(time.Second)
		synctest.Wait()
		retry := s.Status().Providers[0].Inputs[0].Retries[0]
		if calls.Load() != 4 || retry.Attempts != 1 || retry.NextRetry.Sub(time.Now()) != 5*time.Second {
			t.Fatalf("backoff did not reset: %+v", retry)
		}
		s.Close()
	})
}

type retryResolveProvider struct {
	source.Provider
	calls   *atomic.Int32
	failure error
}

func (p retryResolveProvider) Resolve(context.Context, source.ResolveParams) (source.Resolved, error) {
	p.calls.Add(1)
	return source.Resolved{}, p.failure
}

func TestRateLimitBlocksManualPlaybackAcrossRestart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, _ := testService(t, &fakeWallpaperBackend{})
		body := "version=1\n[filters]\nmin_width=2\nmin_height=2\n[[providers]]\nid='wallhaven'\nenabled=true\nrefresh_interval='1s'\n[[providers.searches]]\nid='nature'\nquery='nature'\n"
		if err := os.WriteFile(s.request.ConfigPath, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		config, _, err := LoadConfig(s.request.ConfigPath)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.store.EnsureImage(t.Context(), "wallhaven", "nature", config.InputRevision("wallhaven", "nature"), "", metadataCandidate("remote")); err != nil {
			t.Fatal(err)
		}
		var resolves atomic.Int32
		hint := int64(20)
		provider := retryResolveProvider{Provider: metadataProvider{changedProvider{}}, calls: &resolves, failure: &source.Error{Code: "rate_limited", Message: "SECRET", RetryAfterSeconds: &hint}}
		s.request.Providers = map[string]source.Provider{"wallhaven": provider}
		for range 2 {
			if _, err := s.Action(t.Context(), "next"); err == nil {
				t.Fatal("unavailable remote image applied")
			}
		}
		if resolves.Load() != 1 {
			t.Fatal("manual Next bypassed rate limit")
		}
		s.Close()
		restarted := openTestService(t, s.request, s.resolve, time.Hour, true)
		restarted.StartDiscovery()
		synctest.Wait()
		status := restarted.Status()
		if len(status.Providers[0].Retries) != 1 || status.Providers[0].Retries[0].Failure.Stage != "resolve" || len(status.Providers[0].Inputs[0].Retries) != 1 {
			t.Fatalf("persisted retry health lost: %+v", status)
		}
		if _, err := restarted.Action(t.Context(), "next"); err == nil || resolves.Load() != 1 {
			t.Fatal("restart bypassed limit")
		}
		time.Sleep(19 * time.Second)
		synctest.Wait()
		if _, err := restarted.Action(t.Context(), "next"); err == nil || resolves.Load() != 1 {
			t.Fatal("early retry after restart")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		_, _ = restarted.Action(t.Context(), "next")
		if resolves.Load() != 2 {
			t.Fatalf("retry remained blocked after deadline: %d", resolves.Load())
		}
		restarted.Close()
	})
}

func TestProviderGateProtectsOtherInputsAndNewerLimits(t *testing.T) {
	s, _ := testService(t, &fakeWallpaperBackend{})
	key := catalog.RetryKey{ProviderID: "wallhaven", InputID: "one", Revision: "r1", Scope: "discovery"}
	hint := int64(90)
	s.observeRetry(key, "discovery", &source.Error{Code: "rate_limited", Message: "fixture", RetryAfterSeconds: &hint})
	other := key
	other.InputID = "two"
	other.Revision = "r2"
	if err := s.checkRetry(other, "provider_metadata"); err == nil {
		t.Fatal("another search bypassed provider limit")
	}
	s.observeRetry(other, "discovery", nil)
	if err := s.checkRetry(other, "provider_metadata"); err == nil {
		t.Fatal("older successful call erased provider limit")
	}
	if err := s.checkRetry(other, "provider_download"); err != nil {
		t.Fatal("metadata gate blocked independent original download")
	}
	if err := s.checkRetry(catalog.RetryKey{ProviderID: "local", InputID: "home", Scope: "discovery"}, "provider_metadata"); err != nil {
		t.Fatal("unrelated healthy provider blocked")
	}
}

func TestBadBytesOnlyDelayAffectedCandidateAndPreserveStage(t *testing.T) {
	s, root := testService(t, &fakeWallpaperBackend{})
	refreshForTest(t, s)
	config, _, _ := LoadConfig(s.request.ConfigPath)
	p := config.Providers[0]
	candidates, err := s.store.Candidates(t.Context(), p.ID, p.Folders[0].ID, config.InputRevision(p.ID, p.Folders[0].ID), maxCatalogCandidates)
	if err != nil {
		t.Fatal(err)
	}
	var bad, good catalog.Candidate
	for _, c := range candidates {
		if filepath.Base(c.Candidate.Content.Path) == "a.png" {
			bad = c
		} else {
			good = c
		}
	}
	if err := os.WriteFile(filepath.Join(root, "a.png"), []byte("bad image bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = s.materializeChoice(t.Context(), playbackChoice{identity: bad.Identity}, config)
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != "invalid_content" {
		t.Fatalf("bad bytes category: %v", err)
	}
	key := catalog.RetryKey{ProviderID: p.ID, InputID: bad.Identity.InputID, Revision: bad.Identity.Revision, Scope: "materialization", ImageID: bad.Identity.ImageID}
	if err := s.checkRetry(key, ""); err == nil {
		t.Fatal("bad bytes immediately retried")
	}
	if _, err := s.materializeChoice(t.Context(), playbackChoice{identity: good.Identity}, config); err != nil {
		t.Fatalf("healthy image blocked: %v", err)
	}
	states, err := s.store.LoadRetries(t.Context())
	if err != nil || len(states) != 1 || states[0].Stage != "decode" || states[0].Code != "invalid_content" {
		t.Fatalf("materialization failure conflated: %+v %v", states, err)
	}
	key.Scope = "discovery"
	key.ImageID = 0
	if err := s.checkRetry(key, "provider_metadata"); err != nil {
		t.Fatal("bad bytes blocked metadata refresh")
	}
}

func TestRetryPolicyNonTransientCapAndCancellation(t *testing.T) {
	if got := retryDelay(100, "unavailable", nil); got != 5*time.Minute {
		t.Fatalf("unbounded transient retry: %s", got)
	}
	for _, code := range []string{"auth_required", "auth_rejected", "invalid_config", "unsupported", "not_found", "invalid_content"} {
		if got := retryDelay(1, code, nil); got != time.Hour {
			t.Fatalf("non-transient %s: %s", code, got)
		}
	}
	s, _ := testService(t, &fakeWallpaperBackend{})
	key := catalog.RetryKey{ProviderID: "local", InputID: "home", Revision: "r1", Scope: "discovery"}
	s.observeRetry(key, "discovery", context.Canceled)
	if states, _ := s.store.LoadRetries(t.Context()); len(states) != 0 {
		t.Fatal("shutdown/cancellation persisted as outage")
	}
}

func TestDiscoveryChecksSharedGateBeforeNextPage(t *testing.T) {
	s, root := testService(t, &fakeWallpaperBackend{})
	body := localTOML(root) + "[[providers]]\nid='wallhaven'\nenabled=true\n[[providers.searches]]\nid='nature'\nquery='nature'\n"
	if err := os.WriteFile(s.request.ConfigPath, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	config, _, _ := LoadConfig(s.request.ConfigPath)
	p := config.Providers[1]
	var calls int
	hint := int64(60)
	s.request.Providers = map[string]source.Provider{"wallhaven": metadataProvider{changedProvider{listing: func(context.Context, source.DiscoverParams) (source.Page, error) {
		calls++
		// Model another in-flight input reporting a rate limit after this page.
		s.observeRetry(catalog.RetryKey{ProviderID: p.ID, InputID: "other", Revision: config.InputRevision(p.ID, "nature"), Scope: "discovery"}, "discovery", &source.Error{Code: "rate_limited", Message: "fixture", RetryAfterSeconds: &hint})
		return source.Page{Candidates: []source.Candidate{}, NextCursor: json.RawMessage(`{"page":2}`)}, nil
	}}}}
	_, err := s.refreshInputCounts(t.Context(), p, providerInputs(p, 2, sourceDefaultRules(config))[0], 1000)
	var deferred *retryDeferred
	if calls != 1 || !errors.As(err, &deferred) {
		t.Fatalf("pagination bypassed shared limit: calls=%d err=%v", calls, err)
	}
}

func TestCredentialFailureSharesSlowProviderRetry(t *testing.T) {
	s, _ := testService(t, &fakeWallpaperBackend{})
	key := catalog.RetryKey{ProviderID: "wallhaven", InputID: "one", Revision: "r1", Scope: "materialization", ImageID: 7}
	s.observeRetry(key, "resolve", source.Failure("auth_rejected", "SECRET credential response"))
	other := key
	other.InputID = "two"
	other.ImageID = 8
	if until := s.retryUntil(other, "provider_metadata"); time.Until(until) < 59*time.Minute {
		t.Fatalf("credentials retried rapidly: %v", until)
	}
	providers, _ := s.retrySnapshots()
	health := providers["wallhaven"][0]
	if health.Failure.Stage != "resolve" || health.Failure.Code != "auth_rejected" {
		t.Fatalf("credential stage lost: %+v", health)
	}
}

func TestRetryPersistenceFailureRetainsGateAndStopsService(t *testing.T) {
	s, _ := testService(t, &fakeWallpaperBackend{})
	if err := s.store.Close(); err != nil {
		t.Fatal(err)
	}
	key := catalog.RetryKey{ProviderID: "wallhaven", InputID: "nature", Revision: "r1", Scope: "discovery"}
	s.observeRetry(key, "discovery", source.Failure("rate_limited", "fixture"))
	if err := s.checkRetry(key, "provider_metadata"); err == nil {
		t.Fatal("failed persistence opened retry gate")
	}
	select {
	case err := <-s.Errors():
		if err == nil {
			t.Fatal("missing persistence failure")
		}
	default:
		t.Fatal("service was allowed to continue after persistence failure")
	}
}

func TestCachedSuccessDoesNotResetProviderBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, _ := testService(t, &fakeWallpaperBackend{})
		key := catalog.RetryKey{ProviderID: "wallhaven", InputID: "nature", Revision: "r1", Scope: "materialization", ImageID: 7}
		s.observeRetry(key, "resolve", source.Failure("rate_limited", "fixture"))
		time.Sleep(5 * time.Second)
		cached := key
		cached.ImageID = 8
		s.observeRetry(cached, "materialization", nil)
		// Reusing cached bytes says nothing about the provider's recovery.
		s.observeRetry(key, "resolve", source.Failure("rate_limited", "fixture"))
		retry := s.retries[providerRetryKey("wallhaven", "provider_metadata")]
		if retry.Attempts != 2 || retry.NextAt.Sub(time.Now()) != 10*time.Second {
			t.Fatalf("cache reuse reset provider backoff: %+v", retry)
		}
		s.Close()
	})
}
