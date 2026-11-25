package playback

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/source"
)

const maxDiscoveryWorkers = 4

// StartDiscovery starts one owned coordinator. Close cancels and joins its jobs.
func (s *Service) StartDiscovery() {
	s.discoveryMu.Lock()
	defer s.discoveryMu.Unlock()
	if s.discoveryStarted || s.ctx.Err() != nil {
		return
	}
	s.discoveryStarted = true
	s.statusMu.Lock()
	s.status.Refreshing = true
	s.statusMu.Unlock()
	go s.runDiscovery()
	go s.runThemeDiscovery()
}

type discoverySchedule struct {
	provider        ProviderConfig
	input           providerInput
	next, completed time.Time
	cancel          context.CancelFunc
	health          InputRefreshHealth
}

type discoveryResult struct {
	key    string
	job    *discoverySchedule
	err    error
	counts RefreshCounts
}

func discoveryKey(provider, input string) string { return provider + "/" + input }

// A restart refreshes immediately. Deadlines are completion-relative: missed
// intervals coalesce into one job, with no catch-up burst after sleep or stalls.
func reconcileDiscovery(jobs map[string]*discoverySchedule, config Config, now time.Time) {
	wanted := map[string]bool{}
	for _, p := range config.Providers {
		if !p.Enabled {
			continue
		}
		for _, input := range providerInputs(p, config.Filters.MaxDepth, sourceDefaultRules(config)) {
			if input.disabled {
				continue
			}
			key := discoveryKey(p.ID, input.id)
			wanted[key] = true
			old := jobs[key]
			if old != nil && old.input.revision == input.revision {
				if old.input.interval != input.interval && !old.completed.IsZero() {
					old.next = old.completed.Add(input.interval)
				}
				old.input.interval = input.interval
				continue
			}
			if old != nil && old.cancel != nil {
				old.cancel()
			}
			jobs[key] = &discoverySchedule{provider: p, input: input, next: now, health: InputRefreshHealth{ID: input.id, Revision: input.revision, State: "pending", Interval: input.interval.String()}}
		}
	}
	for key, job := range jobs {
		if !wanted[key] {
			if job.cancel != nil {
				job.cancel()
			}
			delete(jobs, key)
		}
	}
}

func (s *Service) runDiscovery() {
	defer close(s.discoveryDone)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	jobs := map[string]*discoverySchedule{}
	completed := make(chan discoveryResult, maxDiscoveryWorkers)
	// Active keys also include canceled obsolete jobs until they have joined.
	active := map[string]*discoverySchedule{}
	var latestConfig Config
	var configWarning string
	var reclaimCursor catalog.ReclaimCursor
	finish := func(result discoveryResult) {
		delete(active, result.key)
		result.job.cancel()
		result.job.cancel = nil
		s.observeRetry(catalog.RetryKey{ProviderID: result.job.provider.ID, InputID: result.job.input.id, Revision: result.job.input.revision, Scope: "discovery"}, "discovery", result.err)
		if jobs[result.key] != result.job {
			return
		}
		result.job.completed = time.Now()
		result.job.next = result.job.completed.Add(result.job.input.interval)
		if result.err != nil {
			result.job.next = s.retryUntil(catalog.RetryKey{ProviderID: result.job.provider.ID, InputID: result.job.input.id, Revision: result.job.input.revision, Scope: "discovery"}, "provider_metadata")
		}
		job := result.job
		job.health.Counts = &result.counts
		job.health.Failure = refreshFailure(result.err)
		if result.err != nil {
			job.health.State = "failed"
		} else {
			now := time.Now().UTC()
			job.health.LastSuccess = &now
			job.health.State = "ready"
			if result.counts.Inspected == 0 {
				job.health.State = "empty"
			}
		}
		s.notify()
	}
	for {
		config, _, err := LoadConfig(s.request.ConfigPath)
		if err != nil {
			for _, job := range active {
				job.cancel()
			}
			jobs = map[string]*discoverySchedule{}
			configWarning = "discovery configuration invalid; check config.toml"
		} else {
			latestConfig = config
			configWarning = ""
			reconcileDiscovery(jobs, config, time.Now())
			localActive, remoteActive := 0, 0
			for _, job := range active {
				if job.provider.ID == "local" {
					localActive++
				} else {
					remoteActive++
				}
			}
			// Due jobs are ordered by deadline and key so a busy input cannot
			// jump ahead of an input waiting for its reserved worker capacity.
			keys := make([]string, 0, len(jobs))
			for key := range jobs {
				keys = append(keys, key)
			}
			slices.SortFunc(keys, func(a, b string) int {
				if order := jobs[a].next.Compare(jobs[b].next); order != 0 {
					return order
				}
				return strings.Compare(a, b)
			})
			for _, key := range keys {
				job := jobs[key]
				local := job.provider.ID == "local"
				until := s.retryUntil(catalog.RetryKey{ProviderID: job.provider.ID, InputID: job.input.id, Revision: job.input.revision, Scope: "discovery"}, "provider_metadata")
				if active[key] != nil || time.Now().Before(maxTime(job.next, until)) || local && localActive >= 1 || !local && remoteActive >= maxDiscoveryWorkers-1 {
					continue
				}
				ctx, cancel := context.WithTimeout(s.ctx, 3*time.Second)
				job.cancel = cancel
				now := time.Now().UTC()
				job.health.LastAttempt = &now
				job.health.Counts = nil
				job.health.State = "refreshing"
				job.health.Failure = nil
				active[key] = job
				if local {
					localActive++
				} else {
					remoteActive++
				}
				budget := max(1, maxDiscoveryPages/len(jobs))
				p, input := job.provider, job.input
				go func() {
					counts, err := s.refreshInputCounts(ctx, p, input, budget)
					completed <- discoveryResult{key: key, job: job, counts: counts, err: err}
				}()
			}
		}
		s.publishDiscoveryHealth(latestConfig, jobs, len(active) > 0, configWarning)
		select {
		case <-s.ctx.Done():
			for _, job := range active {
				job.cancel()
			}
			for len(active) > 0 {
				finish(<-completed)
			}
			s.publishDiscoveryHealth(latestConfig, jobs, false, configWarning)
			return
		case result := <-completed:
			finish(result)
		case <-ticker.C:
			reclaimCursor = s.reclaimCatalog(reclaimCursor)
		case <-s.discoveryWake:
		}
	}
}

func (s *Service) refreshInputCounts(ctx context.Context, p ProviderConfig, input providerInput, budget int) (RefreshCounts, error) {
	counts := RefreshCounts{Scope: "committed_metadata_in_attempt"}
	provider := s.request.provider(p.ID)
	if provider == nil {
		return counts, fmt.Errorf("provider unavailable")
	}
	key := catalog.RetryKey{ProviderID: p.ID, InputID: input.id, Revision: input.revision, Scope: "discovery"}
	provider = retryProvider{Provider: provider, before: func() error { return s.checkRetry(key, "provider_metadata") }}
	if input.rules != nil {
		provider = &discoveryHintsProvider{
			Provider: provider,
			rules:    *input.rules,
			resolveRatio: func(ctx context.Context, rule *RatioRule) *RatioRule {
				s.statusMu.RLock()
				backend := s.effectiveSettings.Backend
				s.statusMu.RUnlock()
				resolved, _ := s.resolveRatioRule(ctx, rule, backend)
				return resolved
			},
		}
	}

	commit := func(ctx context.Context, scan catalog.Scan, scope string, page source.Page) error {
		err := s.commitDiscoveryPage(ctx, scan, scope, page)
		if err == nil {
			counts.Pages++
			counts.Inspected += len(page.Candidates)
		}
		return err
	}
	err := discoverMetadataWithCommit(ctx, s.store, provider, p.ID, input.id, input.revision, input.scope, input.config, commit, budget)
	return counts, err
}

// Re-read current settings immediately before each page transaction. An old job
// cannot enroll candidates once it observes a disabled, removed or revised input.
// Selection independently checks its own current configuration revision.
func (s *Service) commitDiscoveryPage(ctx context.Context, scan catalog.Scan, scope string, page source.Page) error {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	config, _, err := LoadConfig(s.request.ConfigPath)
	if err != nil {
		return fmt.Errorf("check discovery configuration: %w", err)
	}
	for _, p := range config.Providers {
		if p.ID == scan.ProviderID && p.Enabled && config.InputRevision(p.ID, scan.InputID) == scan.Revision {
			if _, enabled := p.enabledInputRules(scan.InputID); enabled {
				_, err := s.store.CommitPage(ctx, scan, scope, page)
				return err
			}
		}
	}
	return catalog.ErrStaleScan
}
