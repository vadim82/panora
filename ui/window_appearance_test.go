package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWindowPaletteFollowsCurrentOmarchyTheme(t *testing.T) {
	if runtime.GOOS != "linux" {
		if (&Client{}).WindowPalette() != nil {
			t.Fatal("Omarchy window colors are only available on Linux")
		}
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OMARCHY_PATH", "/usr/share/omarchy")
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "test-session")
	root := filepath.Join(home, ".local", "state", "omarchy", "current", "theme")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	client := &Client{}
	if got := client.WindowPalette(); got != nil {
		t.Fatalf("missing file returned %+v", got)
	}
	for _, tc := range []struct {
		name, palette string
		want          *WindowPaletteResult
	}{
		{
			name:    "Ethereal",
			palette: "accent = '#7D82D9'\nbackground = '#060B1E'\nforeground = '#ffcead'\nselection = '#252e56'\nmuted = '#6d7db6'\nred = '#ED5B5A'\nyellow = '#E9BB4F'",
			want: &WindowPaletteResult{
				Accent: "#7d82d9", Background: "#060b1e", Foreground: "#ffcead",
				Selection: "#252e56", Muted: "#6d7db6", Red: "#ed5b5a", Yellow: "#e9bb4f",
			},
		},
		{
			name:    "light theme",
			palette: "accent = '#205EA6'\nbackground = '#FFFCF0'\nforeground = '#100F0F'",
			want:    &WindowPaletteResult{Accent: "#205ea6", Background: "#fffcf0", Foreground: "#100f0f"},
		},
		{name: "malformed TOML", palette: "accent ="},
		{
			name:    "invalid accent",
			palette: "accent = '#zzz'\nbackground = '#123456'",
			want:    &WindowPaletteResult{Background: "#123456"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(root, "colors.toml"), []byte(tc.palette), 0600); err != nil {
				t.Fatal(err)
			}
			got := client.WindowPalette()
			if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
				t.Fatalf("window palette = %+v, want %+v", got, tc.want)
			}
		})
	}
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")
	if got := client.WindowPalette(); got != nil {
		t.Fatalf("non-Omarchy session with stale files returned %+v", got)
	}
}
