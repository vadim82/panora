//go:build !linux

package appservice

import "context"

func ArchPackageContext(ctx context.Context) (context.Context, func(), error) {
	return ctx, func() {}, nil
}
