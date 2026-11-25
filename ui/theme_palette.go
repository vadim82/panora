package main

import (
	"os"
	"runtime"
	"slices"

	"github.com/vadim82/panora/internal/omarchytheme"
	"github.com/vadim82/panora/internal/source"
)

type ThemePaletteResult struct {
	Colors []string `json:"colors,omitempty"`
	Error  string   `json:"error,omitempty"`
}

func (*Client) ThemePalette() ThemePaletteResult {
	unavailable := ThemePaletteResult{Error: "No Omarchy theme palette is available. Choose colors manually."}
	if runtime.GOOS != "linux" {
		return unavailable
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return unavailable
	}
	colors := omarchyPalette(home)
	if len(colors) == 0 {
		return unavailable
	}
	return ThemePaletteResult{Colors: colors}
}

func omarchyPalette(home string) []string {
	palette, err := omarchytheme.ReadCurrentPalette(home)
	if err != nil {
		return nil
	}
	var colors []string
	for _, value := range []string{
		palette.Accent, palette.Background, palette.Foreground, palette.Blue, palette.Green,
		palette.Magenta, palette.Cyan, palette.Red, palette.Yellow,
	} {
		if value != "" && !slices.Contains(colors, value) {
			colors = append(colors, value)
			if len(colors) == source.MaxWallhavenColors {
				break
			}
		}
	}
	return colors
}
