package playback

import (
	"context"
	"encoding/hex"
	"sync"
)

// NavigationRequest makes a window's latest destination relative to the accepted
// cursor. At most two steps remain pending while an apply is in progress.
type NavigationRequest struct {
	Instance string `json:"instance"`
	Current  string `json:"current"`
	Token    string `json:"token"`
	Offset   int    `json:"offset"`
}

type navigationState struct {
	mu       sync.Mutex
	active   *navigationWork
	canceled []string
}

type navigationWork struct {
	token      string
	cancel     context.CancelFunc
	superseded bool
	dispatched bool
}

const maxCanceledNavigations = 16

func ValidNavigationToken(token string) bool {
	if len(token) != 32 {
		return false
	}
	_, err := hex.DecodeString(token)
	return err == nil
}

func (s *Service) Navigate(ctx context.Context, request NavigationRequest) (Status, error) {
	if request.Instance == "" || request.Current != "" && !ValidHistoryReference(request.Current) ||
		!ValidNavigationToken(request.Token) || request.Offset == 0 || request.Offset < -2 || request.Offset > 2 {
		return s.Status(), &Error{Code: "invalid_request", Message: "provide a service instance, cursor, navigation token and an offset of one or two steps"}
	}
	action := "next"
	if request.Offset < 0 {
		action = "previous"
	}
	return s.perform(ctx, action, actionOptions{navigation: &request})
}

func (s *Service) CancelNavigation(instance, token string) error {
	if instance == "" || !ValidNavigationToken(token) {
		return &Error{Code: "invalid_request", Message: "provide a service instance and navigation token"}
	}
	if instance != s.instanceID {
		return &Error{Code: "instance_changed", Message: "the connected Panora service changed before navigation"}
	}
	navigation := &s.navigation
	navigation.mu.Lock()
	defer navigation.mu.Unlock()
	if current := navigation.active; current != nil && current.token == token {
		if !current.dispatched {
			current.superseded = true
			current.cancel()
		}
		return nil
	}
	// The cancellation may arrive just before the original request is handled.
	// Bound these one-use tokens independently of clients and request ordering.
	if len(navigation.canceled) == maxCanceledNavigations {
		navigation.canceled = navigation.canceled[1:]
	}
	navigation.canceled = append(navigation.canceled, token)
	return nil
}

func (s *Service) beginNavigation(ctx context.Context, request NavigationRequest) (context.Context, *navigationWork, *Error) {
	if request.Instance != s.instanceID {
		return ctx, nil, &Error{Code: "instance_changed", Message: "the connected Panora service changed before navigation"}
	}
	current := ""
	if s.state.Cursor >= 0 {
		current = s.state.History[s.state.Cursor].ID
	}
	if current != request.Current {
		return ctx, nil, &Error{Code: "current_changed", Message: "the accepted wallpaper changed before navigation"}
	}
	navigation := &s.navigation
	navigation.mu.Lock()
	defer navigation.mu.Unlock()
	for i, token := range navigation.canceled {
		if token == request.Token {
			navigation.canceled = append(navigation.canceled[:i], navigation.canceled[i+1:]...)
			return ctx, nil, supersededNavigation()
		}
	}
	preparation, cancel := context.WithCancel(ctx)
	work := &navigationWork{token: request.Token, cancel: cancel}
	navigation.active = work
	return preparation, work, nil
}

func (s *Service) finishNavigation(work *navigationWork) bool {
	navigation := &s.navigation
	navigation.mu.Lock()
	defer navigation.mu.Unlock()
	navigation.active = nil
	work.cancel()
	return work.superseded
}

func (s *Service) beginNavigationDispatch(ctx context.Context, work *navigationWork) error {
	navigation := &s.navigation
	navigation.mu.Lock()
	defer navigation.mu.Unlock()
	if work.superseded {
		return supersededNavigation()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	work.dispatched = true
	return nil
}

func supersededNavigation() *Error {
	return &Error{Code: "navigation_superseded", Message: "a newer wallpaper destination replaced this preparation"}
}
