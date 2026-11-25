package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWindowAccentFollowsCurrentOmarchyTheme(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Omarchy session")
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
	for _, tc := range []struct {
		name, palette, accent string
	}{
		{name: "absent"},
		{name: "Ethereal", palette: "accent = '#7D82D9'", accent: "#7d82d9"},
		{name: "Tokyo Night", palette: "accent = '#7aa2f7'", accent: "#7aa2f7"},
		{name: "malformed TOML", palette: "accent ="},
		{name: "malformed accent", palette: "accent = '#zzz'\nblue = '#123456'"},
		{name: "accent absent", palette: "blue = '#123456'"},
		{name: "resumed", palette: "accent = '#123456'", accent: "#123456"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.palette != "" {
				path := filepath.Join(root, "colors.toml")
				if err := os.WriteFile(path, []byte(tc.palette), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if got := client.WindowAccent(); got != tc.accent {
				t.Fatalf("window accent = %q, want %q", got, tc.accent)
			}
		})
	}
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")
	if got := client.WindowAccent(); got != "" {
		t.Fatalf("non-Omarchy session with stale files returned %q", got)
	}
}
