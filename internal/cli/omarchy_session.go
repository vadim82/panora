package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/desktop"
)

func waitForOmarchySession(ctx context.Context, limit time.Duration) error {
	wait, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := checkOmarchySession(wait)
		if err == nil {
			return nil
		}
		select {
		case <-wait.Done():
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		case <-ticker.C:
		}
	}
}

func checkOmarchySession(ctx context.Context) error {
	if _, err := control.SocketPath(""); err != nil {
		return errors.New("XDG_RUNTIME_DIR must identify a private session directory owned by you. Reopen Panora in Omarchy")
	}
	if !filepath.IsAbs(os.Getenv("OMARCHY_PATH")) {
		return errors.New("OMARCHY_PATH is missing or invalid in the systemd user environment. Reopen Panora from your Omarchy session")
	}
	display := os.Getenv("WAYLAND_DISPLAY")
	if display == "" {
		return errors.New("WAYLAND_DISPLAY is missing in the systemd user environment. Reopen Panora from your Omarchy session")
	}
	if !filepath.IsAbs(display) {
		display = filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), display)
	}
	wayland, err := (&net.Dialer{Timeout: 250 * time.Millisecond}).DialContext(ctx, "unix", display)
	if err != nil {
		return errors.New("the Wayland display is unavailable. Wait for Omarchy to finish starting, then try Start again")
	}
	wayland.Close()
	for _, helper := range []string{"omarchy", "omarchy-shell", "qs"} {
		if _, err := exec.LookPath(helper); err != nil {
			return errors.New(helper + " is missing from the systemd user environment. Check the Omarchy installation")
		}
	}
	if err := (desktop.OmarchyBackend{}).Probe(ctx); err != nil {
		return errors.New("the Omarchy background IPC handler is unavailable. Wait for the Omarchy shell, then try Start again")
	}
	return nil
}

func announceSystemdReady(ctx context.Context, path string) error {
	ready, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if _, err := control.Request(ready, path, ""); err != nil {
		return fmt.Errorf("verify the private control API: %w", err)
	}
	return notifySystemd("READY=1")
}

func notifySystemd(message string) error {
	socket := os.Getenv("NOTIFY_SOCKET")
	if socket == "" {
		return nil
	}
	conn, err := (&net.Dialer{Timeout: 200 * time.Millisecond}).Dial("unixgram", socket)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetWriteDeadline(time.Now().Add(200 * time.Millisecond))
	_, err = conn.Write([]byte(message))
	return err
}
