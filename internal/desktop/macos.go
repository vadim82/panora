package desktop

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const macOSHelperName = "panora-macos-wallpaper"

func macOSGeometry(ctx context.Context) (Geometry, error) {
	data, err := runMacOSHelper(ctx, "geometry")
	if err != nil {
		return Geometry{}, err
	}
	// Both adapters publish the same bounded display schema. AppKit has already
	// oriented its frame and converted it to backing pixels; never scale it again.
	return parseHyprlandGeometry([]byte(data))
}

type MacOSBackend struct{}

// ReapplyOnSpaceChange opts into showing the same accepted image on a new Space.
func (MacOSBackend) ReapplyOnSpaceChange() bool { return true }

// SetWallpaper delegates static presentation to AppKit on each connected screen.
// The helper runs on its main thread; no all-Spaces guarantee is implied.
func (MacOSBackend) SetWallpaper(ctx context.Context, path string, options DisplayOptions) error {
	if err := options.Validate(); err != nil {
		return err
	}
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("macOS backend requires macOS")
	}
	fit := options.Fit
	if fit == "" || fit == "auto" {
		fit = "contain"
	}
	upscale := "false"
	if options.Upscale {
		upscale = "true"
	}
	output, err := runMacOSHelper(ctx, "set", path, fit, upscale)
	if err != nil {
		return err
	}
	if strings.TrimSpace(output) != "ok" {
		return fmt.Errorf("macOS helper did not acknowledge the wallpaper request")
	}
	return nil
}

// Probe checks readiness without changing the desktop.
func (MacOSBackend) Probe(ctx context.Context) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("macOS backend requires macOS")
	}
	output, err := runMacOSHelper(ctx, "probe")
	if err != nil {
		return err
	}
	if strings.TrimSpace(output) != "ready" {
		return fmt.Errorf("macOS helper did not report a usable desktop session")
	}
	return nil
}

func findMacOSHelper() (string, error) {
	// Prefer the companion shipped with this executable over another PATH version.
	if executable, err := os.Executable(); err == nil {
		path := filepath.Join(filepath.Dir(executable), macOSHelperName)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return path, nil
		}
	}
	path, err := exec.LookPath(macOSHelperName)
	if err != nil {
		return "", fmt.Errorf("find macOS helper (build tools/macos-wallpaper.swift and place %s beside panora or on PATH): %w", macOSHelperName, err)
	}
	return path, nil
}

func runMacOSHelper(ctx context.Context, args ...string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	executable, err := findMacOSHelper()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := runBackendCommand(ctx, executable, args...)
	if err != nil {
		return "", fmt.Errorf("macOS wallpaper helper: %w", err)
	}
	return output, nil
}
