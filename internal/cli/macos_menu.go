package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

func macOSMenuPath(executable string) string {
	directory := filepath.Dir(executable)
	contents := filepath.Dir(directory)
	if filepath.Base(executable) != "panora" || filepath.Base(directory) != "MacOS" ||
		filepath.Base(contents) != "Contents" || filepath.Ext(filepath.Dir(contents)) != ".app" {
		return ""
	}
	return filepath.Join(contents, "Helpers", "Panora Menu.app", "Contents", "MacOS", "panora-menu")
}

func installedMacOSMenu(socket string, log io.Writer) func(context.Context, string) {
	executable, err := os.Executable()
	if err != nil {
		return nil
	}
	menu := macOSMenuPath(executable)
	if menu == "" {
		return nil
	}
	return func(ctx context.Context, instance string) {
		info, err := os.Lstat(menu)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			fmt.Fprintln(log, "Panora menu helper is missing or not executable. Rebuild or replace the complete Panora app.")
			return
		}
		runMacOSMenu(ctx, menu, socket, instance, log)
	}
}

func runMacOSMenu(ctx context.Context, executable, socket, instance string, log io.Writer) {
	delay := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		child := exec.CommandContext(ctx, executable, "--socket", socket, "--instance", instance)
		// No terminal input or output is inherited. Join the owned helper even
		// if it cannot process a graceful AppKit termination.
		child.Cancel = func() error { return child.Process.Signal(syscall.SIGTERM) }
		child.WaitDelay = 2 * time.Second
		err := child.Run()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			fmt.Fprintln(log, "Panora menu helper exited before the service. Retrying.")
		}
		if time.Since(started) >= 30*time.Second {
			delay = time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay = min(2*delay, 15*time.Second)
	}
}
