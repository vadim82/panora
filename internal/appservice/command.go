package appservice

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"
)

type boundedOutput struct {
	bytes.Buffer
	overflow bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := 32768 - b.Len()
	if len(p) > remaining {
		p = p[:remaining]
		b.overflow = true
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}

func launchctl(ctx context.Context, args ...string) ([]byte, error) {
	return runCommand(ctx, "/bin/launchctl", args...)
}

func runCommand(ctx context.Context, executable string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, args...)
	name := filepath.Base(executable)
	var out boundedOutput
	command.Stdout = &out
	command.Stderr = &out
	command.WaitDelay = 250 * time.Millisecond
	err := command.Run()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("%s %s: %w", name, args[0], ctx.Err())
	}
	if out.overflow {
		return nil, fmt.Errorf("%s %s response exceeded 32 KiB", name, args[0])
	}
	if err != nil {
		return out.Bytes(), fmt.Errorf("%s %s failed: %w", name, args[0], err)
	}
	return out.Bytes(), nil
}
