package desktop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/vadim82/panora/internal/filelock"
)

type OmarchyBackend struct{}

// Probe checks readiness without changing the desktop.
func (OmarchyBackend) Probe(ctx context.Context) error {
	if _, err := exec.LookPath("omarchy"); err != nil {
		return fmt.Errorf("find Omarchy executable: %w", err)
	}
	omarchyPath := os.Getenv("OMARCHY_PATH")
	if !filepath.IsAbs(omarchyPath) {
		return fmt.Errorf("OMARCHY_PATH must identify the installed Omarchy directory with an absolute path")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output, err := runBackendCommand(ctx, "omarchy-shell", "shell", "ping")
	if err != nil {
		return err
	}
	if strings.TrimSpace(output) != "ok" {
		return fmt.Errorf("Omarchy shell did not acknowledge the readiness query")
	}
	// Introspection does not call refresh/set or change the background. Check the
	// actual loaded handler, rather than just an enabled plugin's configuration.
	output, err = runBackendCommand(ctx, "qs", "ipc", "-n", "-p", filepath.Join(omarchyPath, "shell"), "show")
	if err != nil {
		return err
	}
	inBackground := false
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "target ") {
			inBackground = line == "target background"
		} else if inBackground && line == "function set(path: string): void" {
			return nil
		}
	}
	return fmt.Errorf("Omarchy's background IPC handler is unavailable")
}

// SetWallpaper delegates rendering to Omarchy's native background command.
// Fit prepares a padded image; Omarchy still owns the sole rendering surface.
func (backend OmarchyBackend) SetWallpaper(ctx context.Context, path string, options DisplayOptions) error {
	return backend.SetWallpaperWithPreparation(ctx, ctx, path, options, func() error { return nil })
}

// SetWallpaperWithPreparation keeps Fit cancellable until the caller commits
// its intent immediately before sending the desktop command.
func (OmarchyBackend) SetWallpaperWithPreparation(ctx, preparation context.Context, path string, options DisplayOptions, beforeDispatch func() error) error {
	if err := options.Validate(); err != nil {
		return err
	}
	executable, err := exec.LookPath("omarchy")
	if err != nil {
		return fmt.Errorf("find Omarchy executable: %w", err)
	}
	if options.Fit != "crop" {
		dir, err := fitCacheDirectory()
		if err != nil {
			return err
		}
		release, err := filelock.Acquire(preparation, filepath.Join(dir, ".lock"))
		if err != nil {
			return fmt.Errorf("lock Fit cache: %w", err)
		}
		defer release()
		source := path
		if err := pruneFitCache(preparation, dir, fitCacheMaxFiles, fitCacheMaxBytes, source); err != nil {
			return err
		}
		target, err := omarchyFitTarget(preparation)
		if err != nil {
			return err
		}
		path, err = prepareContainedImage(preparation, path, target.Width, target.Height, options.Upscale)
		if err != nil {
			return err
		}
		if err := pruneFitCache(preparation, dir, fitCacheMaxFiles, fitCacheMaxBytes, source, path); err != nil {
			return err
		}
		// A topology change during decoding/scaling invalidates the global canvas.
		current, err := omarchyFitTarget(preparation)
		if err != nil {
			return err
		}
		if current != target {
			return fmt.Errorf("monitor changed during Fit preparation; retry with the current desktop")
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if guard, ok := ctx.Value(omarchyGuardKey{}).(func() bool); ok {
		return applyGuardedOmarchyWallpaper(ctx, preparation, executable, path, guard, beforeDispatch)
	}
	if err := beforeDispatch(); err != nil {
		return err
	}
	if _, err := runBackendCommand(ctx, executable, "theme", "bg", "set", path); err != nil {
		return fmt.Errorf("set wallpaper through Omarchy: %w", err)
	}
	return nil
}

// OmarchyIntroReady waits for the shell's intro/cover to finish by reading its
// status. A shell which has not initialized cannot confirm the handoff.
func OmarchyIntroReady(ctx context.Context, token string) bool {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	output, err := runBackendCommand(ctx, "omarchy-shell", "shell", "themeIntroStatus", token)
	return err == nil && strings.TrimSpace(output) == "ready"
}

var ErrOmarchyThemeChanged = errors.New("Omarchy theme changed before wallpaper dispatch")

type omarchyGuardKey struct{}

// WithOmarchyDispatchGuard protects a selection against native theme changes.
// The guard is checked after Fit preparation under Omarchy's own command lock.
func WithOmarchyDispatchGuard(ctx context.Context, guard func() bool) context.Context {
	return context.WithValue(ctx, omarchyGuardKey{}, guard)
}

func TryOmarchyThemeLock() (func(), error) {
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		runtime = "/tmp"
	}
	if !filepath.IsAbs(runtime) {
		return nil, fmt.Errorf("Omarchy runtime directory must be absolute")
	}
	return filelock.Try(filepath.Join(runtime, "omarchy-theme-set.lock"))
}

func applyGuardedOmarchyWallpaper(ctx, preparation context.Context, executable, path string, guard func() bool, beforeDispatch func() error) error {
	release, err := TryOmarchyThemeLock()
	if err != nil {
		if errors.Is(err, filelock.ErrBusy) {
			return ErrOmarchyThemeChanged
		}
		return err
	}
	defer release()
	if !guard() {
		return ErrOmarchyThemeChanged
	}
	// Omarchy 4.5.0's setter has no lock of its own and uses the native
	// background plugin. A newer shell exposes background sequencing and its
	// matching CLI takes the lock itself. Call the shell directly for that
	// protocol so the generation check remains serialized across dispatch.
	_, capabilityErr := runBackendCommand(preparation, "omarchy-shell", "shell", "themeIntroBusy")
	if capabilityErr != nil && strings.HasSuffix(capabilityErr.Error(), "stderr: Function not found.") {
		if err := beforeDispatch(); err != nil {
			return err
		}
		if _, err := runBackendCommand(ctx, executable, "theme", "bg", "set", path); err != nil {
			return fmt.Errorf("set wallpaper through Omarchy: %w", err)
		}
		return nil
	}
	if capabilityErr != nil {
		return fmt.Errorf("%w: could not confirm the native shell is ready", ErrOmarchyThemeChanged)
	}
	// Panora's hash-named cache images have no native theme intros.
	if err := beforeDispatch(); err != nil {
		return err
	}
	output, err := runBackendCommand(ctx, "omarchy-shell", "shell", "switchBackground", path, "")
	if err != nil {
		return err
	}
	if strings.TrimSpace(output) != "ok" {
		return fmt.Errorf("Omarchy shell did not acknowledge the background switch")
	}
	return nil
}
