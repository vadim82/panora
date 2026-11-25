//go:build wails

package main

import (
	"context"
	"os/exec"
)

func openDesktopLocation(ctx context.Context, location string) error {
	return exec.CommandContext(ctx, "xdg-open", location).Run()
}

func openDesktopConfiguration(ctx context.Context, location string) error {
	if err := openDesktopLocation(ctx, location); err == nil || ctx.Err() != nil {
		return err
	}
	return exec.CommandContext(ctx, "omarchy-launch-editor", location).Run()
}
