//go:build linux || darwin

package source

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestCancellationKillsDescendantProcess(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	client := pythonClient(t, `import os,subprocess,sys,time
p=subprocess.Popen([sys.executable,'-c','import time;time.sleep(60)'])
with open(sys.argv[1],'w') as f: f.write(str(p.pid))
time.sleep(60)
`)
	client.Manifest.Command = append(client.Manifest.Command, pidFile)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := client.Describe(ctx); done <- err }()
	deadline := time.Now().Add(2 * time.Second)
	var pid int
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(pidFile)
		pid, _ = strconv.Atoi(string(data))
		if pid > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if pid <= 0 {
		t.Fatal("child failed to start")
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("operation did not cancel")
	}
	// A killed orphan can briefly remain a zombie until the OS reaps it.
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		data, _ := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
		if len(data) > 0 && containsZombieState(string(data)) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("descendant still running after cancellation")
}

func containsZombieState(stat string) bool {
	for i := len(stat) - 1; i >= 0; i-- {
		if stat[i] == ')' {
			return len(stat) > i+2 && stat[i+2] == 'Z'
		}
	}
	return false
}
