package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/vadim82/panora/internal/playback"
)

// runDesktopEvents coalesces events into one pending refresh and reads the
// latest accepted selection at execution time. Busy playback gets a bounded
// wait, not an unbounded operation queue or a second wallpaper scheduler.
func runDesktopEvents(ctx context.Context, watch func(context.Context, func(), func()) error, refresh func(context.Context) error, warning func(string), out io.Writer) {
	observerCtx, cancel := context.WithCancel(ctx)
	changed := make(chan struct{}, 1)
	updates := make(chan observerUpdate)
	joined := make(chan struct{})
	queueRefresh := func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	}
	go func() {
		defer close(joined)
		reconnectDesktopObserver(observerCtx, watch, queueRefresh, updates)
	}()
	defer func() {
		cancel()
		<-joined
	}()
	reconnecting, catchingUp := false, false
	for {
		select {
		case <-ctx.Done():
			return
		case update := <-updates:
			reconnecting = !update.ready
			if reconnecting {
				warning("Desktop switching is temporarily unavailable. Panora is reconnecting its macOS Space observer.")
				fmt.Fprintf(out, "Desktop Space observer stopped: %v; retrying in %s\n", update.err, update.delay)
			} else {
				warning("")
				fmt.Fprintln(out, "Desktop Space observer reconnected.")
				// Catch up with the latest accepted wallpaper if a switch was
				// missed while the previous observer was unavailable.
				catchingUp = true
				queueRefresh()
			}
		case <-changed:
			// Record each coalesced delivery, not every helper line or busy retry.
			// This distinguishes a silent observer from a completed refresh when
			// investigating a wallpaper that did not visibly follow the Space.
			if catchingUp {
				fmt.Fprintln(out, "Refreshing the current wallpaper after the Space observer reconnected.")
				catchingUp = false
			} else {
				fmt.Fprintln(out, "Desktop Space change observed; refreshing the current wallpaper.")
			}
			started := time.Now()
			deadline := started.Add(30 * time.Second)
			for ctx.Err() == nil {
				// Events already queued before this attempt target the same
				// refresh. An event during the apply can still enqueue another.
				select {
				case <-changed:
				default:
				}
				err := refresh(ctx)
				var failure *playback.Error
				if !errors.As(err, &failure) || failure.Code != "busy" || time.Now().After(deadline) {
					if err != nil && ctx.Err() == nil {
						if !reconnecting {
							warning("The wallpaper could not follow a desktop switch. Inspect the current image and service status, then use Next if needed.")
						}
						fmt.Fprintf(out, "Desktop Space refresh failed: %v\n", err)
					} else if err == nil {
						if !reconnecting {
							warning("")
						}
						// A refresh can be skipped when setup is incomplete or an
						// application is uncertain. Completion never verifies pixels.
						fmt.Fprintf(out, "Desktop Space refresh completed. Elapsed %s.\n", time.Since(started).Round(time.Millisecond))
					}
					break
				}
				timer := time.NewTimer(100 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
				case <-timer.C:
				}
			}
		}
	}
}

type observerUpdate struct {
	ready bool
	err   error
	delay time.Duration
}

// Each watch call owns and joins one helper before it returns. Backoff
// resets after a stable run and bounds repeated failures during login.
func reconnectDesktopObserver(ctx context.Context, watch func(context.Context, func(), func()) error, changed func(), updates chan<- observerUpdate) {
	send := func(update observerUpdate) {
		select {
		case updates <- update:
		case <-ctx.Done():
		}
	}
	delay := time.Second
	retry := false
	for ctx.Err() == nil {
		started := time.Now()
		ready := func() {}
		if retry {
			ready = func() { send(observerUpdate{ready: true}) }
		}
		err := watch(ctx, ready, changed)
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) >= 30*time.Second {
			delay = time.Second
		}
		send(observerUpdate{err: err, delay: delay})
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay = min(2*delay, 30*time.Second)
		retry = true
	}
}
