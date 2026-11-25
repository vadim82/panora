package service

import (
	"context"
	"sync/atomic"

	"github.com/vadim82/panora/internal/desktop"
)

type enrollmentBackend struct{ calls atomic.Int32 }

func (*enrollmentBackend) Probe(context.Context) error { return nil }
func (b *enrollmentBackend) SetWallpaper(context.Context, string, desktop.DisplayOptions) error {
	b.calls.Add(1)
	return nil
}
