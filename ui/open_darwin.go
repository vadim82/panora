//go:build wails

package main

import (
	"context"
	"os/exec"
)

func openDesktopLocation(ctx context.Context, location string) error {
	return exec.CommandContext(ctx, "/usr/bin/open", location).Run()
}

func openDesktopConfiguration(ctx context.Context, location string) error {
	return exec.CommandContext(ctx, "/usr/bin/open", "-t", location).Run()
}
