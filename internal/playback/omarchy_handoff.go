package playback

import (
	"context"
	"errors"
	"time"

	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/filelock"
	"github.com/vadim82/panora/internal/omarchytheme"
)

type themeOperationKey struct{}

type themeOperation struct {
	active    omarchytheme.Active
	automatic bool
}

func staleThemeError() *Error {
	return &Error{Code: "theme_changed", Message: "the active Omarchy theme changed before playback; try again after it settles"}
}

func (s *Service) firstThemeApply(historyLength int) bool {
	return s.theme.freshProfile && historyLength == 0
}

func themeReadyAt(theme *themeObservation, interval time.Duration, firstApply bool) time.Time {
	if firstApply {
		return theme.settled
	}
	return theme.settled.Add(interval)
}

func (s *Service) beginThemeOperation(ctx context.Context, automatic bool) (context.Context, *Error) {
	config, _, err := LoadConfig(s.request.ConfigPath)
	source := config.OmarchyTheme
	enabled := err == nil && source != nil && source.Enabled
	if !enabled || s.theme.home == "" {
		return ctx, nil
	}
	firstApply := automatic && s.firstThemeApply(len(s.state.History)) && !config.hasEnabledProviderInputs()
	active, _ := omarchytheme.ReadActive(s.theme.home)
	if active.Root == "" {
		if firstApply {
			return ctx, &Error{Code: "busy", Message: "waiting for the active Omarchy theme"}
		}
		if automatic {
			release, lockErr := desktop.TryOmarchyThemeLock()
			if errors.Is(lockErr, filelock.ErrBusy) {
				return ctx, &Error{Code: "busy", Message: "waiting for the active Omarchy theme's opening interval"}
			}
			if release != nil {
				release()
			}
			// An absent native theme is itself an observation. Mixed rotation
			// can proceed, but the final lock-protected guard must still detect
			// a theme appearing while the selection was being prepared.
			return context.WithValue(ctx, themeOperationKey{}, themeOperation{automatic: true}), nil
		}
		return ctx, nil
	}
	if automatic {
		theme := s.theme.observation.Load()
		if theme == nil || !theme.enabled || theme.active != active || theme.settled.IsZero() || time.Now().Before(themeReadyAt(theme, s.interval, firstApply)) {
			return ctx, &Error{Code: "busy", Message: "waiting for the active Omarchy theme's opening interval"}
		}
		if firstApply {
			collection := s.theme.collection.Load()
			if collection == nil || collection.active != active || collection.sourceID != source.ID {
				return ctx, &Error{Code: "busy", Message: "waiting for Omarchy theme wallpaper discovery"}
			}
		}
	}
	return context.WithValue(ctx, themeOperationKey{}, themeOperation{active, automatic}), nil
}

func (s *Service) guardThemeChoice(ctx context.Context, choice playbackChoice) (context.Context, func() bool, *Error) {
	theme, hasTheme := ctx.Value(themeOperationKey{}).(themeOperation)
	if choice.identity.ProviderID == omarchyProvider {
		if !hasTheme || theme.active.ID != choice.identity.Revision {
			return ctx, nil, staleThemeError()
		}
	} else if !hasTheme || !theme.automatic {
		return ctx, nil, nil
	}
	guard := func() bool {
		active, _ := omarchytheme.ReadActive(s.theme.home)
		if active != theme.active {
			return false
		}
		if choice.identity.ProviderID == omarchyProvider {
			_, err := s.themeStillFor(choice.identity)
			return err == nil
		}
		return true
	}
	return desktop.WithOmarchyDispatchGuard(ctx, guard), guard, nil
}

func (s *Service) isActiveThemeHistory(identity catalog.Identity) bool {
	_, err := s.themeStillFor(identity)
	return err == nil
}
