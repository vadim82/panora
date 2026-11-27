package appservice

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// WatchHomebrewUpgrade follows the Homebrew link only for the exact managed
// launchd process. A CLI foreground service never takes over another keg.
func WatchHomebrewUpgrade(ctx context.Context, socket string) <-chan string {
	if os.Getenv("XPC_SERVICE_NAME") != DefaultLabel {
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return nil
	}
	launcher := HomebrewLaunchPath(executable)
	if launcher == executable {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	manager, err := New(Options{Executable: launcher, Home: home, Socket: socket})
	if err != nil {
		return nil
	}
	registration, found, err := manager.readOwned(manager.registration)
	if err != nil || !found || registration.Arguments[0] != launcher {
		return nil
	}
	_, owned, pid, err := manager.jobProcess(ctx)
	if err != nil || !owned || pid != os.Getpid() {
		return nil
	}
	keg, _ := homebrewKeg(executable)
	return watchHomebrewLink(ctx, launcher, filepath.Dir(keg), executable, 2*time.Second)
}

// ExecHomebrewService replaces the drained service in the same launchd job.
func ExecHomebrewService(executable string) error {
	return syscall.Exec(executable, append([]string{executable}, os.Args[1:]...), os.Environ())
}
