package main

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/vadim82/panora/internal/omarchytheme"
)

type WindowPaletteResult struct {
	Background string `json:"background"`
	Foreground string `json:"foreground"`
	Accent     string `json:"accent"`
	Muted      string `json:"muted"`
	Selection  string `json:"selection"`
	Red        string `json:"red"`
	Yellow     string `json:"yellow"`
}

// WindowPalette reads window colors independently of the service and search drafts.
func (*Client) WindowPalette() *WindowPaletteResult {
	if runtime.GOOS != "linux" || !filepath.IsAbs(os.Getenv("OMARCHY_PATH")) || os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") == "" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	palette, err := omarchytheme.ReadCurrentPalette(home)
	if err != nil {
		return nil
	}
	return &WindowPaletteResult{
		Background: palette.Background, Foreground: palette.Foreground, Accent: palette.Accent,
		Muted: palette.Muted, Selection: palette.Selection, Red: palette.Red, Yellow: palette.Yellow,
	}
}
