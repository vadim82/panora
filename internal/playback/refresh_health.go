package playback

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/vadim82/panora/internal/cache"
	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/source"
)

const maxHealthInputs = 512

// Provider-supplied text and raw errors are never public diagnostics.
func failureForCode(stage, code string) *RefreshFailure {
	messages := map[string]string{
		"unavailable": "provider is unavailable", "rate_limited": "provider rate limit reached",
		"auth_required": "provider authentication is required", "auth_rejected": "provider authentication was rejected",
		"invalid_config": "provider settings are invalid", "unsupported": "provider operation or content is unsupported",
		"not_found": "provider input or image was not found", "timeout": "provider operation timed out", "canceled": "provider operation was canceled",
		"download_failed": "original download failed", "invalid_content": "original bytes are invalid or exceed image limits",
		"filter_mismatch": "decoded image does not match current image rules", "cache_failed": "image cache operation failed",
		"resolve_failed": "original resolution failed", "discovery_failed": "provider discovery failed",
	}
	message, ok := messages[code]
	if !ok {
		code = "discovery_failed"
		message = messages[code]
	}
	return &RefreshFailure{Stage: stage, Code: code, Message: message}
}

func refreshFailure(err error) *RefreshFailure {
	if err == nil {
		return nil
	}
	stage, code := "discovery", "discovery_failed"
	var staged *stageError
	if errors.As(err, &staged) {
		stage = staged.stage
	}
	var cached *cache.Error
	if errors.As(err, &cached) {
		stage = cached.Stage
		code = cached.Code
	}
	if code == "discovery_failed" {
		switch stage {
		case "resolve":
			code = "resolve_failed"
		case "download":
			code = "download_failed"
		case "cache", "materialization":
			code = "cache_failed"
		case "filter":
			code = "filter_mismatch"
		}
	}
	var typed *source.Error
	if errors.As(err, &typed) {
		switch typed.Code {
		case "unavailable", "rate_limited", "auth_required", "auth_rejected", "invalid_config", "unsupported", "not_found":
			code = typed.Code
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		code = "timeout"
	} else if errors.Is(err, context.Canceled) {
		code = "canceled"
	}
	return failureForCode(stage, code)
}

func healthPriority(state string) int {
	switch state {
	case "failed":
		return 5
	case "refreshing":
		return 4
	case "pending":
		return 3
	case "ready":
		return 2
	case "empty":
		return 1
	}
	return 0
}

// Only the coordinator reads mutable jobs. Published snapshots are immutable;
// status never waits for providers, image inspection, or SQL.
func (s *Service) publishDiscoveryHealth(config Config, jobs map[string]*discoverySchedule, running bool, configWarning string) {
	if configWarning == "" {
		s.setSetupRequired(!config.HasEnabledInputs())
	}
	providers := make([]ProviderRefreshHealth, 0, len(config.Providers))
	warnings := []string{}
	if configWarning != "" {
		warnings = append(warnings, configWarning)
	}
	providerRetries, inputRetries := s.retrySnapshots()
	remaining := maxHealthInputs
	for _, p := range config.Providers {
		inputs := providerInputs(p, config.Filters.MaxDepth, sourceDefaultRules(config))
		summary := ProviderRefreshHealth{ID: p.ID, Enabled: p.Enabled, State: "disabled", TotalInputs: len(inputs), Inputs: []InputRefreshHealth{}, Retries: providerRetries[p.ID]}
		if p.Enabled && len(inputs) == 0 {
			summary.State = "empty"
		}
		for _, input := range inputs {
			health := InputRefreshHealth{ID: input.id, Revision: input.revision, State: "disabled", Interval: input.interval.String()}
			if p.Enabled && !input.disabled {
				health.State = "pending"
				if configWarning != "" {
					health.State = "failed"
					health.Failure = &RefreshFailure{Stage: "configuration", Code: "invalid_config", Message: configWarning}
				}
				if job := jobs[discoveryKey(p.ID, input.id)]; job != nil {
					health = job.health
					health.Interval = job.input.interval.String()
					health.Retries = inputRetries[discoveryKey(p.ID, input.id)+"/"+input.revision]
					until := s.retryUntil(catalog.RetryKey{ProviderID: p.ID, InputID: input.id, Revision: input.revision, Scope: "discovery"}, "provider_metadata")
					if time.Now().Before(until) && job.cancel == nil {
						health.State = "failed"
						for _, retry := range append(append([]RetryHealth{}, health.Retries...), summary.Retries...) {
							if retry.Scope == "discovery" || retry.Scope == "provider_metadata" {
								copy := retry.Failure
								health.Failure = &copy
							}
						}
					}
					if job.cancel == nil {
						next := maxTime(job.next, until).UTC()
						health.NextRefresh = &next
					}
				}
			}
			if healthPriority(health.State) > healthPriority(summary.State) {
				summary.State = health.State
			}
			if health.Failure != nil && len(warnings) < 20 {
				warnings = append(warnings, fmt.Sprintf("%s/%s [%s]: %s", p.ID, input.id, health.Failure.Code, health.Failure.Message))
			}
			if remaining > 0 {
				summary.Inputs = append(summary.Inputs, health)
				remaining--
			} else {
				summary.OmittedInputs++
			}
		}
		providers = append(providers, summary)
	}
	s.statusMu.Lock()
	s.status.RefreshHealthVersion = RefreshHealthVersion
	s.status.Providers = providers
	s.status.Refreshing = running
	s.status.RefreshWarnings = warnings
	s.statusMu.Unlock()
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneProviderHealth(providers []ProviderRefreshHealth) []ProviderRefreshHealth {
	if providers == nil {
		return nil
	}
	result := append([]ProviderRefreshHealth(nil), providers...)
	for i := range result {
		result[i].Retries = append([]RetryHealth(nil), providers[i].Retries...)
		result[i].Inputs = make([]InputRefreshHealth, len(providers[i].Inputs))
		copy(result[i].Inputs, providers[i].Inputs)
		for j := range result[i].Inputs {
			h := &result[i].Inputs[j]
			h.Retries = append([]RetryHealth(nil), h.Retries...)
			h.LastAttempt = cloneTime(h.LastAttempt)
			h.LastSuccess = cloneTime(h.LastSuccess)
			h.NextRefresh = cloneTime(h.NextRefresh)
			if h.Failure != nil {
				copy := *h.Failure
				h.Failure = &copy
			}
			if h.Counts != nil {
				copy := *h.Counts
				h.Counts = &copy
				if copy.KnownEligible != nil {
					n := *copy.KnownEligible
					h.Counts.KnownEligible = &n
				}
			}
		}
	}
	return result
}
