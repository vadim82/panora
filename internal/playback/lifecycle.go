package playback

import (
	"context"
	"fmt"
	"time"

	"github.com/vadim82/panora/internal/filelock"
)

type ServiceOptions struct {
	Request         Request
	Interval        time.Duration
	IntervalChanged bool
	Paused          bool
	Backend         string
	// FreshOmarchyProfile lets a newly seeded theme start after the native intro.
	FreshOmarchyProfile bool
}

// OpenService exclusively owns state and scheduling until Close. It does not
// open a socket, start a listener, or install desktop/session configuration.
func OpenService(ctx context.Context, options ServiceOptions, resolve Resolver) (*Service, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if options.Request.Source == nil {
		return nil, fmt.Errorf("source provider is required")
	}
	if options.IntervalChanged && options.Interval <= 0 {
		return nil, fmt.Errorf("rotation interval must be positive")
	}
	if err := options.Request.Options.Validate(); err != nil {
		return nil, err
	}
	config, _, err := LoadConfig(options.Request.ConfigPath)
	if err != nil {
		return nil, err
	}
	if !options.IntervalChanged {
		options.Interval, _ = time.ParseDuration(config.Playback.Interval)
	}
	if options.Request.StatePath == "" {
		path, err := DefaultStatePath()
		if err != nil {
			return nil, err
		}
		options.Request.StatePath = path
	}
	releaseScheduler, err := filelock.Try(options.Request.StatePath + ".rotation.lock")
	if err != nil {
		return nil, fmt.Errorf("another service owns this state: %w", err)
	}
	releaseState, err := LockState(options.Request.StatePath)
	if err != nil {
		releaseScheduler()
		return nil, err
	}
	service, err := newService(ctx, options, resolve, config)
	if err != nil {
		releaseState()
		releaseScheduler()
		return nil, err
	}
	service.release = func() { releaseState(); releaseScheduler() }
	return service, nil
}

// Stop cancels and joins playback work while retaining the store and ownership.
// The service composer drains transport handlers before calling Close.
func (s *Service) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.discoveryMu.Lock()
	started := s.discoveryStarted
	lookaheadDone := s.lookaheadDone
	s.discoveryMu.Unlock()
	if started {
		<-s.discoveryDone
		<-s.theme.done
	}
	if lookaheadDone != nil {
		<-lookaheadDone
	}
	s.actionMu.Lock()
	s.publishStatus()
	s.actionMu.Unlock()
}

// Close retains ownership until rendering and discovery have stopped.
func (s *Service) Close() {
	s.closeOnce.Do(func() {
		s.Stop()
		s.actionMu.Lock()
		defer s.actionMu.Unlock()
		if s.cache != nil {
			s.cache.Close()
		}
		if s.store != nil {
			s.store.Close()
		}
		if s.release != nil {
			s.release()
		}
	})
}

func (s *Service) Errors() <-chan error { return s.fatal }
