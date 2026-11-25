package playback

import (
	"context"
	"time"

	"github.com/vadim82/panora/internal/cache"
)

type CacheStatus struct {
	Version        int          `json:"version"`
	Saved          cache.Limits `json:"saved"`
	Effective      cache.Limits `json:"effective"`
	Usage          cache.Usage  `json:"usage"`
	LastApplyError *Error       `json:"last_apply_error,omitempty"`
	UsageError     *Error       `json:"usage_error,omitempty"`
}

// Callers hold actionMu while reading current/pending protection or changing
// budgets. Cache.mu independently serializes reads, publication and eviction.
func (s *Service) protectedCachePaths() []string {
	paths := []string{s.state.CurrentPath()}
	if s.state.Pending != nil {
		paths = append(paths, s.state.Pending.Path)
	}
	return paths
}

// Settings have already been published. Cleanup cannot roll back that save;
// report active limits and any cleanup failure independently of persistence.
func (s *Service) applyCachePreferences(config Config, request UpdateConfigurationRequest) {
	if request.CacheMaxImages == nil && request.CacheMaxBytes == nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	snapshot, err := s.cache.UpdateLimits(ctx, config.Cache.MaxImages, config.Cache.MaxBytes, s.protectedCachePaths()...)
	s.statusMu.Lock()
	s.effectiveSettings.CacheMaxImages = snapshot.Limits.MaxImages
	s.effectiveSettings.CacheMaxBytes = snapshot.Limits.MaxBytes
	s.statusMu.Unlock()
	s.cacheApplyError = nil
	if err != nil {
		s.cacheApplyError = &Error{Code: "cache_cleanup_failed", Message: "saved cache limits are active, but cleanup did not finish; inspect cache usage and retry the saved limits"}
		if snapshot.Limits.MaxImages != config.Cache.MaxImages || snapshot.Limits.MaxBytes != config.Cache.MaxBytes {
			s.cacheApplyError = &Error{Code: "cache_limits_pending", Message: "cache limits were saved but could not be activated; retry the saved limits"}
		}
	}
}

// CacheStatus reports unique managed files, not SQL memberships. A usage error
// is explicit rather than presenting zero files as a successful measurement.
func (s *Service) CacheStatus(ctx context.Context) (CacheStatus, error) {
	if !s.actionMu.TryLock() {
		return CacheStatus{}, &Error{Code: "busy", Message: "another playback operation is running; try again after it finishes"}
	}
	defer s.actionMu.Unlock()
	if ctx.Err() != nil || s.ctx.Err() != nil {
		return CacheStatus{}, &Error{Code: "canceled", Message: "cache request canceled or service stopping"}
	}
	config, _, err := LoadConfig(s.request.ConfigPath)
	if err != nil {
		return CacheStatus{}, invalidSavedConfiguration()
	}
	operation, cancel := context.WithTimeout(ctx, 5*time.Second)
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	defer cancel()
	snapshot, err := s.cache.Usage(operation, s.protectedCachePaths()...)
	var lastApplyError *Error
	if s.cacheApplyError != nil {
		copy := *s.cacheApplyError
		lastApplyError = &copy
	}
	result := CacheStatus{Version: StatusVersion, Saved: cache.Limits{MaxImages: config.Cache.MaxImages, MaxBytes: config.Cache.MaxBytes}, Effective: snapshot.Limits, Usage: snapshot.Usage, LastApplyError: lastApplyError}
	if err != nil {
		result.UsageError = &Error{Code: "cache_usage_unavailable", Message: "could not inspect playback cache usage; counters are unknown"}
	}
	return result, nil
}
