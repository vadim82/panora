package main

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/vadim82/panora/internal/omarchytheme"
)

// WindowAccent is a read-only appearance hint independent of the service.
func (*Client) WindowAccent() string {
	if runtime.GOOS != "linux" || !filepath.IsAbs(os.Getenv("OMARCHY_PATH")) || os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	palette, err := omarchytheme.ReadCurrentPalette(home)
	if err != nil {
		return ""
	}
	return palette.Accent
}
