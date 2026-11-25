package playback

import "time"

// Called by the configuration writer after publication, while actionMu is held.
// Only explicit interval edits replace a temporary startup interval override.
func (s *Service) applyRuntimePreferences(config Config, request UpdateConfigurationRequest) {
	if request.Interval != nil {
		interval, _ := time.ParseDuration(config.Playback.Interval) // Already validated.
		if interval != s.interval {
			s.interval = interval
			s.nextAt = time.Now().Add(interval)
			s.statusMu.Lock()
			s.effectiveSettings.Interval = interval.String()
			s.statusMu.Unlock()
			s.publishStatus()
			s.notify()
		}
	}
	s.applyCachePreferences(config, request)
}
