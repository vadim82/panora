//go:build darwin || linux

package appservice

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vadim82/panora/internal/filelock"
)

const homebrewServiceSuffix = "/libexec/Panora.app/Contents/MacOS/panora"

func homebrewKeg(executable string) (string, bool) {
	keg, ok := strings.CutSuffix(executable, homebrewServiceSuffix)
	rack := filepath.Dir(keg)
	return keg, ok && filepath.IsAbs(keg) && filepath.Base(rack) == "panora" &&
		filepath.Base(filepath.Dir(rack)) == "Cellar"
}

// HomebrewLaunchPath uses the formula's stable wrapper, which executes a
// versioned binary so the service and its bundled helpers stay together.
func HomebrewLaunchPath(executable string) string {
	keg, ok := homebrewKeg(executable)
	if !ok {
		return executable
	}
	rack := filepath.Dir(keg)
	prefix := filepath.Dir(filepath.Dir(rack))
	launcher := filepath.Join(prefix, "opt", "panora", "bin", "panora")
	if linkedHomebrewService(launcher, rack) == "" {
		return executable
	}
	return launcher
}

func linkedHomebrewService(launcher, rack string) string {
	wrapper, err := filepath.EvalSymlinks(launcher)
	if err != nil {
		return ""
	}
	keg := filepath.Dir(filepath.Dir(wrapper))
	if filepath.Dir(keg) != rack || wrapper != filepath.Join(keg, "bin", "panora") {
		return ""
	}
	executable := keg + homebrewServiceSuffix
	if info, err := os.Lstat(executable); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
		return executable
	}
	return ""
}

// stopLegacyHomebrew runs only on explicit Start from the Homebrew app. It
// allows an open of the first supporting version to adopt an older running
// job. Start keeps the saved registration's existing RunAtLoad value.
func (m *Manager) stopLegacyHomebrew(ctx context.Context) (bool, error) {
	launcher := m.options.Executable
	if !strings.HasSuffix(launcher, "/opt/panora/bin/panora") {
		return false, nil
	}
	current, found, err := m.readOwned(m.registration)
	if err != nil || !found {
		return false, err
	}
	old := current.Arguments[0]
	if old == launcher || HomebrewLaunchPath(old) != launcher {
		return false, nil
	}
	loaded, owned, pid, err := m.jobProcess(ctx)
	if err != nil || !loaded || !owned || pid <= 0 {
		return false, err
	}
	if _, err := m.command(ctx, "bootout", m.target); err != nil {
		return false, fmt.Errorf("stop the old Homebrew service: %w", err)
	}
	release, err := filelock.Acquire(ctx, m.options.Socket+".lock")
	if err != nil {
		return false, fmt.Errorf("wait for the old Homebrew service: %w", err)
	}
	release()
	return true, nil
}

func watchHomebrewLink(ctx context.Context, launcher, rack, executable string, interval time.Duration) <-chan string {
	next := make(chan string, 1)
	go func() {
		timer := time.NewTicker(interval)
		defer timer.Stop()
		for {
			if linked := linkedHomebrewService(launcher, rack); linked != "" && linked != executable {
				next <- linked
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
		}
	}()
	return next
}
