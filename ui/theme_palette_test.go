package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestOmarchyPaletteReadsCurrentThemeOnDemand(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".local/state/omarchy/current/theme/colors.toml")
	if colors := omarchyPalette(home); len(colors) != 0 {
		t.Fatalf("missing theme reported %v", colors)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	setTheme := func(contents string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	setTheme("accent = '#0066CC'\nbackground = '#424153'\nblue = '#0066cc'\nforeground = 'invalid'\ncolor7 = '#fefefe'\n")
	if got := omarchyPalette(home); !slices.Equal(got, []string{"#0066cc", "#424153", "#fefefe"}) {
		t.Fatalf("theme palette = %v", got)
	}
	setTheme("accent = '#66cccc'\n")
	if got := omarchyPalette(home); !slices.Equal(got, []string{"#66cccc"}) {
		t.Fatalf("changed theme palette = %v", got)
	}
	setTheme("invalid TOML")
	if colors := omarchyPalette(home); len(colors) != 0 {
		t.Fatalf("malformed theme reported %v", colors)
	}
}
