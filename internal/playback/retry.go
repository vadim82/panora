package playback

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/source"
)

type stageError struct {
	stage string
	err   error
}

func (e *stageError) Error() string { return e.stage + " failed" }
func (e *stageError) Unwrap() error { return e.err }
func atStage(stage string, err error) error {
	if err == nil {
		return nil
	}
	var existing *stageError
	if errors.As(err, &existing) {
		return err
	}
	return &stageError{stage, err}
}

type retryDeferred struct{ until time.Time }

func (e *retryDeferred) Error() string {
	return "network retry deferred until " + e.until.UTC().Format(time.RFC3339)
}

func retryDelay(attempts int, code string, hint *int64) time.Duration {
	delay := min(5*time.Second<<min(max(attempts-1, 0), 6), 5*time.Minute)
	switch code {
	case "auth_required", "auth_rejected", "invalid_config", "unsupported", "not_found", "invalid_content", "filter_mismatch":
		delay = time.Hour
	}
	if hint != nil && *hint >= 0 {
		delay = max(delay, time.Duration(min(*hint, 86400))*time.Second)
	}
	return delay
}

func providerRetryKey(provider, scope string) catalog.RetryKey {
	return catalog.RetryKey{ProviderID: provider, Scope: scope}
}

func (s *Service) retryUntil(key catalog.RetryKey, providerScope string) time.Time {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()
	until := s.retries[key].NextAt
	if providerScope != "" {
		until = maxTime(until, s.retries[providerRetryKey(key.ProviderID, providerScope)].NextAt)
	}
	return until
}

func maxTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return b
	}
	return a
}

func (s *Service) checkRetry(key catalog.RetryKey, providerScope string) error {
	if until := s.retryUntil(key, providerScope); time.Now().Before(until) {
		return &retryDeferred{until}
	}
	return nil
}

func (s *Service) wakeDiscovery() {
	select {
	case s.discoveryWake <- struct{}{}:
	default:
	}
}

// Persist learned delays before another call can pass the gate. All disk work
// is outside status locks and provider calls, with a separate finite context.
func (s *Service) observeRetry(key catalog.RetryKey, stage string, err error) {
	var deferred *retryDeferred
	if errors.As(err, &deferred) || errors.Is(err, context.Canceled) {
		return
	}
	s.retryMu.Lock()
	defer s.retryMu.Unlock()
	if err == nil {
		s.deleteRetryLocked(key)
		if stage == "discovery" {
			s.clearProviderRetryLocked(key.ProviderID, "provider_metadata")
		}

		s.wakeDiscovery()
		return
	}
	failure := refreshFailure(atStage(stage, err))
	if failure.Code == "filter_mismatch" {
		return
	}
	var typed *source.Error
	var hint *int64
	if errors.As(err, &typed) && (typed.Code == "rate_limited" || typed.Code == "unavailable") {
		hint = typed.RetryAfterSeconds
	}
	states := []catalog.RetryState{s.retryStateLocked(key, failure, hint)}
	if failure.Code == "rate_limited" || failure.Code == "auth_required" || failure.Code == "auth_rejected" || hint != nil && *hint > 0 {
		scope := "provider_metadata"
		if failure.Stage == "download" || failure.Stage == "decode" {
			scope = "provider_download"
		}
		states = append(states, s.retryStateLocked(providerRetryKey(key.ProviderID, scope), failure, hint))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.store.SaveRetries(ctx, states); err != nil {
		s.failRetryStore(err)
	}
	s.wakeDiscovery()
}

func (s *Service) retryStateLocked(key catalog.RetryKey, failure *RefreshFailure, hint *int64) catalog.RetryState {
	previous := s.retries[key]
	attempts := min(previous.Attempts+1, 16)
	next := maxTime(previous.NextAt, time.Now().UTC().Add(retryDelay(attempts, failure.Code, hint)))
	rounded := next.Truncate(time.Millisecond)
	if rounded.Before(next) {
		rounded = rounded.Add(time.Millisecond)
	}
	state := catalog.RetryState{RetryKey: key, Attempts: attempts, NextAt: rounded, Stage: failure.Stage, Code: failure.Code}
	// Keep the in-memory gate closed even if persistence fails; stop the service
	// rather than continue with retry limits that could be bypassed by restart.
	s.retries[key] = state
	return state
}

func (s *Service) deleteRetryLocked(key catalog.RetryKey) {
	if _, ok := s.retries[key]; !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.store.DeleteRetry(ctx, key); err != nil {
		s.failRetryStore(err)
		return
	}
	delete(s.retries, key)
}

func (s *Service) clearProviderRetryLocked(provider, scope string) {
	key := providerRetryKey(provider, scope)
	if state, ok := s.retries[key]; ok && !time.Now().Before(state.NextAt) {
		s.deleteRetryLocked(key)
	}
}

func (s *Service) clearProviderRetry(provider, scope string) {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()
	s.clearProviderRetryLocked(provider, scope)
}

func (s *Service) failRetryStore(err error) {
	select {
	case s.fatal <- fmt.Errorf("retry state could not be saved; stopping service: %w", err):
	default:
	}
}

// Aggregate candidate retries without exposing internal SQL image IDs. A single
// representative per scope bounds health regardless of the number of bad images.
func (s *Service) retrySnapshots() (map[string][]RetryHealth, map[string][]RetryHealth) {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()
	groups := map[string]map[string]RetryHealth{}
	for key, state := range s.retries {
		if !time.Now().Before(state.NextAt) {
			continue
		}
		group := key.ProviderID
		if key.InputID != "" {
			group = discoveryKey(key.ProviderID, key.InputID) + "/" + key.Revision
		}
		if groups[group] == nil {
			groups[group] = map[string]RetryHealth{}
		}
		old, ok := groups[group][key.Scope]
		count := old.AffectedImages
		if key.ImageID != 0 {
			count++
		}
		if !ok || state.NextAt.Before(old.NextRetry) {
			old = RetryHealth{Scope: key.Scope, Attempts: state.Attempts, NextRetry: state.NextAt.UTC(), Failure: *failureForCode(state.Stage, state.Code)}
		}
		old.AffectedImages = count
		groups[group][key.Scope] = old
	}
	providers, inputs := map[string][]RetryHealth{}, map[string][]RetryHealth{}
	for key, scopes := range groups {
		for _, scope := range []string{"provider_metadata", "provider_download", "discovery", "materialization"} {
			if health, ok := scopes[scope]; ok {
				if scope == "provider_metadata" || scope == "provider_download" {
					providers[key] = append(providers[key], health)
				} else {
					inputs[key] = append(inputs[key], health)
				}
			}
		}
	}
	return providers, inputs
}
