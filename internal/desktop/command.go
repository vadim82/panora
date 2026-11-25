package desktop

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// runBackendCommand bounds both output streams and cancels commands on overflow.
// Callers set the deadline; WaitDelay also bounds inherited output pipes.
func runBackendCommand(ctx context.Context, name string, args ...string) (string, error) {
	executable, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("find %s executable: %w", name, err)
	}
	commandCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, executable, args...)
	cmd.WaitDelay = time.Second
	stdout := &commandOutput{limit: 64 << 10, cancel: cancel}
	stderr := &commandOutput{limit: 4 << 10, cancel: cancel}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err = cmd.Run()
	if stdout.exceeded || stderr.exceeded {
		return "", fmt.Errorf("run %s: output limit exceeded", name)
	}
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("run %s: %w", name, ctx.Err())
		}
		if _, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("run %s: %w; stderr: %s", name, err, strings.TrimSpace(stderr.String()))
		}
		return "", fmt.Errorf("run %s: %w", name, err)
	}
	return stdout.String(), nil
}

type commandOutput struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
	cancel   context.CancelFunc
}

func (out *commandOutput) Write(data []byte) (int, error) {
	remaining := out.limit - out.buffer.Len()
	if len(data) > remaining {
		out.buffer.Write(data[:remaining])
		out.exceeded = true
		out.cancel()
		return len(data), nil
	}
	return out.buffer.Write(data)
}

func (out *commandOutput) String() string { return out.buffer.String() }
