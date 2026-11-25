package playback

import (
	"context"
	"fmt"
	"time"
)

// SetDesktopWarning exposes a session observer failure through existing status
// warnings. It does not change accepted playback or persistent user settings.
func (s *Service) SetDesktopWarning(message string) {
	s.statusMu.Lock()
	s.desktopWarning = message
	s.statusMu.Unlock()
}

// RefreshDesktop reapplies the latest accepted image after a desktop event.
// There is no selection, provider call, history navigation or timer reset.
// The action lock prevents an older selection from overwriting a concurrent Next.
func (s *Service) RefreshDesktop(ctx context.Context) (result error) {
	if !s.actionMu.TryLock() {
		return &Error{Code: "busy", Message: "playback is busy; desktop refresh is pending"}
	}
	defer s.actionMu.Unlock()
	if ctx.Err() != nil || s.ctx.Err() != nil {
		return &Error{Code: "canceled", Message: "desktop refresh canceled or service stopping"}
	}
	if s.state.Pending != nil || s.state.Cursor < 0 || s.Status().SetupRequired {
		return nil
	}
	operation, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	defer func() {
		if result != nil {
			if failure, ok := result.(*Error); ok {
				s.lastError = failure
			}
		}
		s.busy = false
		s.publishStatus()
		s.notify()
	}()
	backend, err := s.resolveDesktop(operation)
	if err != nil {
		return &Error{Code: "desktop_refresh_failed", Message: fmt.Sprintf("could not refresh the desktop: %v", err)}
	}
	aware, ok := backend.(interface{ ReapplyOnSpaceChange() bool })
	if !ok || !aware.ReapplyOnSpaceChange() {
		return nil
	}
	s.stopLookaheadWork()
	entry := s.state.History[s.state.Cursor]
	// The action lock preserves current-file protection against cache eviction
	// through the entire apply. Reuse validates bytes without remote recovery.
	if err := s.cache.VerifyAccepted(operation, entry.Path, s.currentHash); err != nil {
		return &Error{Code: "desktop_refresh_failed", Message: "the current cached image is unavailable; use Next to select another wallpaper"}
	}
	s.busy = true
	s.publishStatus()
	if err := s.applyDesktop(operation, backend, playbackChoice{path: entry.Path, hash: s.currentHash, identity: entry.Identity, sourceID: entry.SourceID, historyIndex: s.state.Cursor}, true); err != nil {
		return err
	}
	if s.lastError != nil && s.lastError.Code == "desktop_refresh_failed" {
		s.lastError = nil
	}
	return nil
}
