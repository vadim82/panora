package omarchytheme

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Palette contains normalized colors from an Omarchy theme's colors.toml.
type Palette struct {
	Accent, Background, Foreground          string
	Muted, Selection                        string
	Blue, Green, Magenta, Cyan, Red, Yellow string
}

// ReadCurrentPalette reads the current Omarchy palette, including the legacy
// config location on installations without a state theme.
func ReadCurrentPalette(home string) (Palette, error) {
	state := filepath.Join(home, ".local/state/omarchy/current")
	root := filepath.Join(state, "theme")
	if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) {
		// A named state theme can briefly lack its directory during an Omarchy
		// switch. Keep the legacy config from masquerading as the current theme.
		if _, err := os.Lstat(filepath.Join(state, "theme.name")); errors.Is(err, os.ErrNotExist) {
			root = filepath.Join(home, ".config/omarchy/current/theme")
		}
	}
	return ReadPalette(root)
}

// ReadPalette reads the regular colors.toml file in an active theme directory.
func ReadPalette(root string) (Palette, error) {
	const maxBytes = 32 << 10
	path := filepath.Join(root, "colors.toml")
	info, err := os.Stat(path)
	if err != nil {
		return Palette{}, fmt.Errorf("stat theme palette: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > maxBytes {
		return Palette{}, fmt.Errorf("theme palette is not a bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return Palette{}, fmt.Errorf("open theme palette: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return Palette{}, fmt.Errorf("read theme palette: %w", err)
	}
	if len(data) > maxBytes {
		return Palette{}, fmt.Errorf("theme palette is too large")
	}
	var values map[string]any
	if err := toml.Unmarshal(data, &values); err != nil {
		return Palette{}, fmt.Errorf("invalid theme palette: %w", err)
	}
	color := func(keys ...string) string {
		for _, key := range keys {
			value, _ := values[key].(string)
			if len(value) == 7 && value[0] == '#' {
				if _, err := hex.DecodeString(value[1:]); err == nil {
					return strings.ToLower(value)
				}
			}
		}
		return ""
	}
	return Palette{
		Accent: color("accent"), Background: color("background", "bg", "color0"),
		Foreground: color("foreground", "fg", "color7"),
		Muted:      color("muted", "dark_foreground"), Selection: color("selection"), Blue: color("blue", "color4"),
		Green: color("green", "color2"), Magenta: color("magenta", "purple", "color5"),
		Cyan: color("cyan", "color6"), Red: color("red", "color1"), Yellow: color("yellow", "color3"),
	}, nil
}
