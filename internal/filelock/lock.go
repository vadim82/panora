// Package filelock provides advisory process locks for Panora state and caches.
package filelock

import (
	"context"
	"errors"
	"time"
)

var ErrBusy = errors.New("another Panora operation holds the lock")

// Acquire waits for a lock, or returns when the context is canceled.
func Acquire(ctx context.Context, path string) (func(), error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		release, err := Try(path)
		if !errors.Is(err, ErrBusy) {
			return release, err
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
