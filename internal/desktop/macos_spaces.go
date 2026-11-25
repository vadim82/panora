package desktop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const spaceObserverStartupTimeout = 5 * time.Second

// WatchMacOSSpaces streams public AppKit notifications for the service lifetime.
// It never sets wallpapers. Startup, line size, stderr and child shutdown are
// bounded; the caller coalesces events instead of queuing playback operations.
func WatchMacOSSpaces(ctx context.Context, ready, changed func()) error {
	executable, err := findMacOSHelper()
	if err != nil {
		return err
	}
	executable, err = macOSSpaceHelper(executable)
	if err != nil {
		return err
	}
	return watchSpaces(ctx, executable, ready, changed)
}

// A packaged observer must initialize AppKit under its own bundle identity.
// Launching the companion directly from Panora.app registers a second Panora
// application, which can receive Finder/Dock activation instead of the client.
// Standalone CLI helpers have no containing app identity and remain supported.
func macOSSpaceHelper(companion string) (string, error) {
	directory := filepath.Dir(companion)
	contents := filepath.Dir(directory)
	if filepath.Base(directory) != "MacOS" || filepath.Base(contents) != "Contents" || filepath.Ext(filepath.Dir(contents)) != ".app" {
		return companion, nil
	}
	observer := filepath.Join(contents, "Helpers", "Panora Space Observer.app", "Contents", "MacOS", macOSHelperName)
	info, err := os.Stat(observer)
	if err != nil {
		return "", fmt.Errorf("find dedicated macOS Space observer (rebuild Panora.app): %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return "", errors.New("dedicated macOS Space observer is not an executable file; rebuild Panora.app")
	}
	return observer, nil
}

func watchSpaces(ctx context.Context, executable string, ready, changed func()) error {
	child, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	startup := time.AfterFunc(spaceObserverStartupTimeout, func() { cancel(context.DeadlineExceeded) })
	defer startup.Stop()
	stopChild := func() { cancel(context.Canceled) }
	output := &spaceEvents{onReady: ready, changed: changed, cancel: stopChild, startup: startup, window: time.Now()}
	stderr := &commandOutput{limit: 4096, cancel: stopChild}
	command := exec.CommandContext(child, executable, "watch-spaces")
	command.WaitDelay = time.Second
	command.Stdout, command.Stderr = output, stderr
	err := command.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if output.err != nil {
		return output.err
	}
	if stderr.exceeded {
		return errors.New("desktop Space observer exceeded its diagnostic output limit")
	}
	if errors.Is(context.Cause(child), context.DeadlineExceeded) {
		return fmt.Errorf("desktop Space observer did not report readiness within %s: %w", spaceObserverStartupTimeout, context.DeadlineExceeded)
	}
	if !output.ready {
		if err != nil {
			return fmt.Errorf("desktop Space observer exited before readiness: %w", err)
		}
		return errors.New("desktop Space observer exited before readiness")
	}
	if err != nil {
		return fmt.Errorf("desktop Space observer stopped: %w", err)
	}
	return errors.New("desktop Space observer stopped")
}

// os/exec serializes writes to this stream and joins its copy goroutine. It
// bounds an incomplete line as well as each complete event, without accumulating
// output over a long-running service lifetime.
type spaceEvents struct {
	pending []byte
	ready   bool
	onReady func()
	err     error
	changed func()
	cancel  context.CancelFunc
	startup *time.Timer
	window  time.Time
	count   int
}

func (s *spaceEvents) Write(data []byte) (int, error) {
	total := len(data)
	if s.err != nil {
		return 0, s.err
	}
	for _, value := range data {
		if value != '\n' {
			if len(s.pending) >= 64 {
				s.fail("desktop Space observer event exceeded 64 bytes")
				return total, nil
			}
			s.pending = append(s.pending, value)
			continue
		}
		event := string(s.pending)
		s.pending = s.pending[:0]
		if !s.ready {
			if event != "ready" {
				s.fail("desktop Space observer did not report readiness")
				return total, nil
			}
			s.ready = true
			s.startup.Stop()
			s.onReady()
			continue
		}
		if event != "changed" {
			s.fail("desktop Space observer returned an invalid event")
			return total, nil
		}
		if time.Since(s.window) >= time.Second {
			s.window, s.count = time.Now(), 0
		}
		s.count++
		if s.count > 100 {
			s.fail("desktop Space observer exceeded its event limit")
			return total, nil
		}
		s.changed()
	}
	return total, nil
}
func (s *spaceEvents) fail(message string) { s.err = errors.New(message); s.cancel() }
