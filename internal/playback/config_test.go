package playback

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func localTOML(root string) string {
	return fmt.Sprintf("version = 1\n[[providers]]\nid = 'local'\nenabled = true\n[[providers.folders]]\nid = 'home'\npath = %q\n", root)
}

func writeConfigBody(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTOMLConfigDefaultsAndValidation(t *testing.T) {
	root := t.TempDir()
	valid := localTOML(root)
	config, ratio, err := LoadConfig(writeConfigBody(t, "# Comments are supported\n"+valid))
	if err != nil || ratio != nil || config.Filters.MaxDepth != 2 || config.Filters.MinWidth != 1 || config.Filters.MinHeight != 1 || config.Filters.Ratio != "any" {
		t.Fatalf("defaults: %+v %v %v", config, ratio, err)
	}
	for _, body := range []string{
		"", "version = 2", "version = '1'", "version = 1\nunknown = true", valid + "[filters]\nmin_width=0", valid + "[filters]\nratio='bad'", valid + "[filters]\nmax_depth=-1", valid + "[filters]\nunknown=1", valid + "[filters]\nmin_width='wide'",
		strings.Replace(valid, "id = 'local'", "id = 'missing'", 1), strings.Replace(valid, fmt.Sprintf("path = %q", root), "path = 'relative'", 1),
		valid + "[[providers.folders]]\nid='home'\npath='/tmp'", valid + "[[providers]]\nid='local'\nenabled=false",
		valid + "[[providers]]\nid='wallhaven'\nenabled=false\n[[providers.searches]]\nid='home'\nquery='mountains'", valid + "[[providers]]\nid='wallhaven'\nenabled=false\n[[providers.searches]]\nid='search'\nquery='one'\nquery='two'",
		valid + "[[providers.searches]]\nid='search'\nquery='mountains'", strings.Replace(valid, "id = 'home'", "id = '  '", 1), valid + "credential = 'secret'",
	} {
		if _, _, err := LoadConfig(writeConfigBody(t, body)); err == nil {
			t.Fatalf("accepted invalid configuration: %s", body)
		}
	}
}

func TestDefaultInitializationPreservesExplicitSavedImageFilters(t *testing.T) {
	for _, test := range []struct {
		name          string
		width, height int
		ratio         string
	}{
		{"previous defaults", 5120, 1440, "any"},
		{"custom ultrawide", 5120, 1440, "32:9"},
		{"custom portrait", 1080, 1920, "9:16"},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := fmt.Sprintf("# Keep this reviewed image rule.\nversion = 1\n[filters]\nmin_width = %d\nmin_height = %d\nratio = %q\n", test.width, test.height, test.ratio)
			path := writeConfigBody(t, body)
			if created, err := InitializeDefaultConfig(path, false); err != nil || created {
				t.Fatalf("existing configuration initialized: created=%v error=%v", created, err)
			}
			config, ratio, err := LoadConfig(path)
			if err != nil || config.Filters.MinWidth != test.width || config.Filters.MinHeight != test.height || config.Filters.Ratio != test.ratio || (ratio == nil) != (test.ratio == "any") {
				t.Fatalf("saved image rule changed: %+v %v %v", config.Filters, ratio, err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != body {
				t.Fatalf("existing TOML changed: %q %v", data, err)
			}
		})
	}
}

func TestProviderSettingsAndDiscoveryRevisions(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	body := localTOML(first) + fmt.Sprintf("[[providers.folders]]\nid='work'\npath=%q\n", second) + "[[providers]]\nid='wallhaven'\nenabled=false\n[[providers.searches]]\nid='mountains'\nquery='mountains'\n[[providers.searches]]\nid='space'\nquery='space'\n"
	path := writeConfigBody(t, body)
	config, _, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	folders, revision := config.localFolders()
	if len(folders) != 2 || len(config.Providers[1].Searches) != 2 || !strings.HasPrefix(revision, "input-v1-") {
		t.Fatalf("settings lost: %+v", config)
	}
	again, _, err := LoadConfig(path)
	if err != nil || again.InputRevision("local", "home") != revision {
		t.Fatal("unchanged settings changed revision")
	}
	filtered, _, err := LoadConfig(writeConfigBody(t, body+"[filters]\nmin_width=100"))
	if err != nil || filtered.InputRevision("local", "home") == revision {
		t.Fatal("inherited filter hints reused discovery revision")
	}
	depth, _, err := LoadConfig(writeConfigBody(t, body+"[filters]\nmax_depth=1"))
	if err != nil || depth.InputRevision("local", "home") == revision {
		t.Fatal("depth change reused discovery revision")
	}
	renamed, _, err := LoadConfig(writeConfigBody(t, strings.Replace(body, "id='work'", "id='renamed'", 1)))
	if err != nil || renamed.InputRevision("local", "home") != revision {
		t.Fatal("renaming a sibling changed the home revision")
	}
	search := config.InputRevision("wallhaven", "mountains")
	edited, _, err := ParseConfig([]byte(strings.Replace(body, "query='space'", "query='stars'", 1)))
	if err != nil || edited.InputRevision("wallhaven", "mountains") != search ||
		edited.InputRevision("wallhaven", "space") == config.InputRevision("wallhaven", "space") {
		t.Fatalf("editing one search did not preserve its sibling: %v", err)
	}
}

func TestConfigPathAndOldJSONRejection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	defaultPath, err := configPath("")
	if err != nil || !strings.HasSuffix(defaultPath, filepath.Join("panora", "config.toml")) {
		t.Fatalf("default: %s %v", defaultPath, err)
	}
	dir := filepath.Dir(defaultPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(dir, "config.json")
	old := []byte(`{"version":1}`)
	if err := os.WriteFile(oldPath, old, 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(oldPath); _ = os.Remove(defaultPath) })
	if _, _, err := LoadConfig(oldPath); err == nil || !strings.Contains(err.Error(), ".toml") {
		t.Fatalf("old explicit path: %v", err)
	}
	if _, err := configPath(""); err == nil {
		t.Fatal("old default silently accepted")
	}
	if err := os.WriteFile(defaultPath, []byte(localTOML(t.TempDir())), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := configPath(""); err == nil {
		t.Fatal("conflicting defaults silently selected")
	}
	if _, _, err := LoadConfig(defaultPath); err != nil {
		t.Fatalf("explicit TOML must select it: %v", err)
	}
	after, _ := os.ReadFile(oldPath)
	if string(after) != string(old) {
		t.Fatal("old configuration was changed")
	}
}

func TestConfiguredPlaybackInterval(t *testing.T) {
	base := localTOML(t.TempDir())
	for _, interval := range []string{"", "0s", "-1m", "later"} {
		if _, _, err := LoadConfig(writeConfigBody(t, base+fmt.Sprintf("[playback]\ninterval=%q\n", interval))); err == nil {
			t.Fatalf("accepted interval %q", interval)
		}
	}
	config, _, err := LoadConfig(writeConfigBody(t, base+"[playback]\ninterval='45m'\n"))
	if err != nil || config.Playback.Interval != "45m" {
		t.Fatalf("interval: %+v %v", config.Playback, err)
	}
}

func TestPlaybackCacheBudgets(t *testing.T) {
	base := localTOML(t.TempDir())
	for _, settings := range []string{"max_images=0", "max_images=-1", "max_bytes=0", "max_bytes=-1"} {
		if _, _, err := LoadConfig(writeConfigBody(t, base+"[cache]\n"+settings)); err == nil {
			t.Fatalf("accepted %s", settings)
		}
	}
	c, _, err := LoadConfig(writeConfigBody(t, base+"[cache]\nmax_images=3\nmax_bytes=1048576"))
	if err != nil || c.Cache.MaxImages != 3 || c.Cache.MaxBytes != 1048576 {
		t.Fatalf("budgets: %+v %v", c.Cache, err)
	}
}

func TestRemoteOnlyProviderConfiguration(t *testing.T) {
	body := "version=1\n[[providers]]\nid='wallhaven'\nenabled=true\n[[providers.searches]]\nid='mountains'\nquery='mountains'\n"
	c, _, err := LoadConfig(writeConfigBody(t, body))
	if err != nil || len(c.Providers) != 1 || !c.Providers[0].Enabled {
		t.Fatalf("remote only: %+v %v", c, err)
	}
}

func TestRefreshIntervalSettings(t *testing.T) {
	base := localTOML(t.TempDir())
	original, _, _ := LoadConfig(writeConfigBody(t, base))
	for _, value := range []string{"0s", "-1m", "later", "999999999999h", "1ms"} {
		for _, body := range []string{base + fmt.Sprintf("refresh_interval=%q", value), strings.Replace(base, "enabled = true", fmt.Sprintf("enabled = true\nrefresh_interval=%q", value), 1)} {
			if _, _, err := LoadConfig(writeConfigBody(t, body)); err == nil {
				t.Fatalf("accepted refresh interval %s", value)
			}
		}
	}
	body := strings.Replace(base, "enabled = true", "enabled = true\nrefresh_interval='2h'", 1) + "refresh_interval='10m'"
	config, _, err := LoadConfig(writeConfigBody(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if config.InputRevision("local", "home") != original.InputRevision("local", "home") {
		t.Fatal("scheduling changed discovery identity")
	}
	inputs := providerInputs(config.Providers[0], config.Filters.MaxDepth, sourceDefaultRules(config))
	if inputs[0].interval.String() != "10m0s" {
		t.Fatalf("input override: %v", inputs[0].interval)
	}
	config.Providers[0].Folders[0].RefreshInterval = ""
	if got := providerInputs(config.Providers[0], 2, sourceDefaultRules(config))[0].interval; got.String() != "2h0m0s" {
		t.Fatalf("provider default: %v", got)
	}
}

func TestDesktopConfigDefaultsAndValidation(t *testing.T) {
	base := localTOML(t.TempDir())
	config, _, err := LoadConfig(writeConfigBody(t, base))
	if err != nil || config.Desktop.Backend != "auto" || config.Desktop.Fit != "auto" || config.Desktop.Upscale {
		t.Fatalf("desktop defaults: %+v %v", config.Desktop, err)
	}
	for _, backend := range []string{"auto", "omarchy", "noctalia", "macos"} {
		config, _, err = LoadConfig(writeConfigBody(t, base+"\n[desktop]\nbackend="+strconv.Quote(backend)+"\nfit='contain'\nupscale=true\n"))
		if err != nil || config.Desktop.Backend != backend || !config.Desktop.Upscale {
			t.Fatalf("desktop settings: %+v %v", config.Desktop, err)
		}
	}
	for _, settings := range []string{"backend=''", "backend='other'", "backend=1", "fit='stretch'", "fit='crop'\nupscale=true", "upscale='yes'", "unknown=true"} {
		if _, _, err := LoadConfig(writeConfigBody(t, base+"\n[desktop]\n"+settings)); err == nil {
			t.Fatalf("invalid desktop settings accepted: %s", settings)
		}
	}
}
