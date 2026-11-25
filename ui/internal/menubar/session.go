// Package menubar is the read-only status and explicit-action client behind the macOS menu.
package menubar

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/filelock"
)

type Action string

const (
	Previous Action = "previous"
	Pause    Action = "pause"
	Resume   Action = "resume"
	Next     Action = "next"
)

type State struct {
	Message                             string
	Previous, Next, Pause, Paused, Stop bool
}

func menuState(status control.Status) State {
	state := State{Stop: true, Paused: status.Paused}
	navigate := !status.Busy && !status.SetupRequired
	state.Previous, state.Next = navigate && status.HistoryCursor > 0, navigate
	state.Pause = navigate && !status.Uncertain
	switch {
	case status.Busy:
		state.Message = "Panora — operation in progress"
	case status.SetupRequired:
		state.Message = "Panora — add a wallpaper source"
	case status.Uncertain:
		state.Message = "Panora — inspect uncertain wallpaper request"
	case status.Paused:
		state.Message = "Panora — rotation paused"
	default:
		state.Message = "Panora — rotation running"
	}
	return state
}

func (s State) allows(action Action) bool {
	switch action {
	case Previous:
		return s.Previous
	case Next:
		return s.Next
	case Pause:
		return s.Pause && !s.Paused
	case Resume:
		return s.Pause && s.Paused
	default:
		return false
	}
}

type Session struct {
	socket, instance string
	initial          control.Status
	release          func()
}

// Connect requires a live private socket and the instance that launched the
// helper. Acquire the singleton lock before any AppKit initialization.
func Connect(ctx context.Context, socket, instance string) (*Session, error) {
	if instance == "" || len(instance) > 128 {
		return nil, fmt.Errorf("an observed service instance is required")
	}
	session := &Session{socket: socket, instance: instance}
	status, err := session.status(ctx)
	if err != nil {
		return nil, err
	}
	release, err := filelock.Try(socket + ".menu.lock")
	if err != nil {
		return nil, err
	}
	session.initial, session.release = status, release
	return session, nil
}

func (s *Session) Close() { s.release() }

func (s *Session) status(ctx context.Context) (control.Status, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	status, err := control.Request(ctx, s.socket, "")
	if err != nil {
		return control.Status{}, err
	}
	if status.InstanceID != s.instance {
		return control.Status{}, fmt.Errorf("the Panora service changed")
	}
	return status, nil
}

// Run observes a single service. Stop uses a separate channel so an explicit
// shutdown can interrupt a slow playback action. Mutations are never replayed.
func (s *Session) Run(ctx context.Context, actions <-chan Action, stops <-chan struct{}, update func(State)) {
	ctx, cancel := context.WithCancel(ctx)
	var worker sync.WaitGroup
	defer func() {
		cancel()
		worker.Wait()
	}()

	var playback <-chan control.Status
	var confirmFailed bool
	publish := func(status control.Status) {
		state := menuState(status)
		if playback != nil {
			state.Previous, state.Next, state.Pause = false, false, false
			state.Message = "Panora — operation in progress"
		}
		if confirmFailed {
			state.Message = "Panora — action could not be confirmed"
		}
		update(state)
	}
	refresh := func() bool {
		status, err := s.status(ctx)
		if err != nil {
			return false
		}
		publish(status)
		return true
	}
	publish(s.initial)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			confirmFailed = false
			if !refresh() {
				return
			}
		case _, open := <-stops:
			if !open {
				stops = nil
				continue
			}
			if ctx.Err() != nil {
				return
			}
			update(State{Message: "Panora — stopping service"})
			// The service signals its companion immediately after flushing
			// the acknowledgement. Give this read time to finish on SIGTERM.
			stopCtx, stopCancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			result, err := control.ShutdownInstance(stopCtx, s.socket, s.instance)
			stopCancel()
			if err == nil && result.Stopping && result.InstanceID == s.instance {
				return
			}
			confirmFailed = true
			if !refresh() {
				return
			}
		case action, open := <-actions:
			if !open {
				return
			}
			if playback != nil {
				continue
			}
			status, err := s.status(ctx)
			if err != nil {
				return
			}
			confirmFailed = false
			if !menuState(status).allows(action) {
				publish(status)
				continue
			}
			result := make(chan control.Status, 1)
			playback = result
			worker.Go(func() {
				response, _ := control.RequestInstance(ctx, s.socket, string(action), s.instance)
				result <- response
			})
			publish(status)
		case result := <-playback:
			playback = nil
			if result.InstanceID == s.instance {
				publish(result)
				continue
			}
			confirmFailed = true
			if !refresh() {
				return
			}
		}
	}
}
