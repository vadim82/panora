package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/vadim82/panora/internal/playback"
)

func TestDesktopEventsReportDeliveryFailureAndRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var output bytes.Buffer
	warnings := make(chan string, 2)
	events := make(chan struct{})
	watch := func(ctx context.Context, ready, changed func()) error {
		ready()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-events:
				changed()
			}
		}
	}
	calls := 0
	refresh := func(context.Context) error {
		calls++
		if calls == 1 {
			return errors.New("fake refresh failure")
		}
		return nil
	}
	done := make(chan struct{})
	go func() {
		runDesktopEvents(ctx, watch, refresh, func(message string) { warnings <- message }, &output)
		close(done)
	}()
	for _, wantWarning := range []bool{true, false} {
		select {
		case events <- struct{}{}:
		case <-time.After(2 * time.Second):
			t.Fatal("observer did not accept event")
		}
		select {
		case message := <-warnings:
			if (message != "") != wantWarning {
				t.Fatalf("warning on failure/recovery: %q", message)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("refresh did not finish")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("observer did not join")
	}
	if calls != 2 || strings.Count(output.String(), "Desktop Space change observed;") != 2 || strings.Count(output.String(), "Desktop Space refresh completed.") != 1 || !strings.Contains(output.String(), "Desktop Space refresh failed: fake refresh failure") {
		t.Fatalf("delivery/refresh evidence missing: calls=%d output=%s", calls, output.String())
	}
}

func TestDesktopEventsMergeBusyBurstsButRetainSwitchesDuringApply(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		emit := make(chan func(), 1)
		watch := func(ctx context.Context, ready, changed func()) error {
			ready()
			emit <- changed
			<-ctx.Done()
			return ctx.Err()
		}
		calls := 0
		release := make(chan struct{})
		refresh := func(ctx context.Context) error {
			calls++
			switch calls {
			case 1:
				return &playback.Error{Code: "busy"}
			case 3:
				select {
				case <-release:
				case <-ctx.Done():
				}
			}
			return ctx.Err()
		}
		done := make(chan struct{})
		go func() {
			runDesktopEvents(ctx, watch, refresh, func(string) {}, io.Discard)
			close(done)
		}()
		changed := <-emit
		changed()
		synctest.Wait()
		if calls != 1 {
			t.Fatalf("expected one busy attempt, got %d", calls)
		}
		for range 1000 {
			changed()
		}
		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		if calls != 2 {
			t.Fatalf("busy burst triggered extra applies: %d attempts", calls)
		}
		changed()
		synctest.Wait()
		if calls != 3 {
			t.Fatalf("expected an apply for a later switch, got %d attempts", calls)
		}
		changed()
		close(release)
		synctest.Wait()
		if calls != 4 {
			t.Fatalf("expected a follow-up for the switch during apply, got %d attempts", calls)
		}
		cancel()
		<-done
		changed()
		if calls != 4 {
			t.Fatalf("observer delivered an apply after shutdown, got %d attempts", calls)
		}
	})
}

func TestDesktopEventsReconnectAfterReadinessTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		var output bytes.Buffer
		warnings := make(chan string, 4)
		var attempts, calls atomic.Int32
		emit := make(chan func(), 1)
		done := make(chan struct{})
		go func() {
			defer close(done)
			runDesktopEvents(ctx, func(ctx context.Context, ready, changed func()) error {
				if attempts.Add(1) == 1 {
					return context.DeadlineExceeded
				}
				ready()
				emit <- changed
				<-ctx.Done()
				return ctx.Err()
			}, func(context.Context) error {
				calls.Add(1)
				return nil
			}, func(message string) { warnings <- message }, &output)
		}()
		if warning := <-warnings; !strings.Contains(warning, "reconnecting") || calls.Load() != 0 {
			t.Fatalf("observer failure: refreshes=%d warning=%q", calls.Load(), warning)
		}
		time.Sleep(time.Second)
		changed := <-emit
		synctest.Wait()
		if warning := <-warnings; attempts.Load() != 2 || calls.Load() != 1 || warning != "" {
			t.Fatalf("observer recovery: attempts=%d refreshes=%d warning=%q", attempts.Load(), calls.Load(), warning)
		}
		changed()
		synctest.Wait()
		if calls.Load() != 2 {
			t.Fatalf("reconnected observer did not deliver subsequent Space changes: %d", calls.Load())
		}
		cancel()
		<-done
		if !strings.Contains(output.String(), "retrying in 1s") || !strings.Contains(output.String(), "Desktop Space observer reconnected.") {
			t.Fatalf("missing observer diagnostics: %s", output.String())
		}
	})
}

func TestDesktopObserverBackoffStopsWithService(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		updates := make(chan observerUpdate)
		done := make(chan struct{})
		go func() {
			defer close(done)
			reconnectDesktopObserver(ctx, func(context.Context, func(), func()) error {
				return errors.New("fake observer exit")
			}, func() {}, updates)
		}()
		for _, delay := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second} {
			if update := <-updates; update.ready || update.delay != delay {
				t.Fatalf("observer retry = %+v, want %s", update, delay)
			}
		}
		cancel()
		<-done
	})
}
