package playback

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/omarchytheme"
)

const fixtureStaticWebP = "UklGRiIAAABXRUJQVlA4TBYAAAAvP8AIAAcQBSxg/wNAQvi/Xozof+oB"
const fixtureAnimatedWebP = "UklGRoQAAABXRUJQVlA4WAoAAAACAAAAPwAAIwAAQU5JTQYAAAAAAAAAAABBTk1GKAAAAAAAAAAAAD8AACMAAGQAAAJWUDhMEAAAAC8/wAgABxAFLGD/AxHR/wBBTk1GKAAAAAAAAAAAAD8AACMAAGQAAABWUDhMEAAAAC8/wAgAB9DS/kf/AxHR/wA="

func fixtureFile(t *testing.T, name string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, content, 0600); err != nil {
		t.Fatal(err)
	}
}

func setFixtureTheme(t *testing.T, home, name, generation string) string {
	t.Helper()
	current := filepath.Join(home, ".local", "state", "omarchy", "current")
	fixtureFile(t, filepath.Join(current, "theme.name"), []byte(name+"\n"))
	fixtureFile(t, filepath.Join(current, "theme.generation"), []byte(generation+"\n"))
	root := filepath.Join(current, "theme")
	fixtureFile(t, filepath.Join(root, "colors.toml"), []byte("accent='#7E83DC'\nbackground='#070b1a'\nforeground='#ffcaad'\n"))
	return root
}

func fixtureWebP(t *testing.T, path, encoded string) {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	fixtureFile(t, path, data)
}

func fixtureThemeImage(t *testing.T, path string, tint byte, encodeJPEG bool) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	pixels := image.NewRGBA(image.Rect(0, 0, 64, 36))
	pixels.Set(0, 0, color.RGBA{R: tint, G: 77, B: 143, A: 255})
	if encodeJPEG {
		err = jpeg.Encode(f, pixels, nil)
	} else {
		err = png.Encode(f, pixels)
	}
	if closeErr := f.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func collectFixtureTheme(t *testing.T, s *Service, id string) {
	t.Helper()
	active, err := omarchytheme.ReadActive(s.theme.home)
	if err != nil {
		t.Fatal(err)
	}
	discovery := s.discoverTheme(t.Context(), active, id, nil, nil)
	if discovery.collection == nil {
		t.Fatal("theme did not prepare a collection")
	}
	s.theme.collection.Store(discovery.collection)
}

func TestOmarchyCollectionBoundsRulesAndPlayback(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Omarchy is a Linux source")
	}
	s, _, backend := configurationFixture(t, `version=1
[filters]
min_width=5120
min_height=1440
ratio="32:9"
`)
	s.theme.home = t.TempDir()
	s.draw = func(int) int { return 0 }
	root := setFixtureTheme(t, s.theme.home, "ethereal", "a1")
	stills := filepath.Join(root, "backgrounds")
	userStills := filepath.Join(s.theme.home, ".config", "omarchy", "backgrounds", "ethereal")
	fixtureWebP(t, filepath.Join(userStills, "00-user.webp"), fixtureStaticWebP)
	fixtureWebP(t, filepath.Join(stills, "animated.webp"), fixtureAnimatedWebP)
	fixtureThemeImage(t, filepath.Join(stills, "00-bundled.jpg"), 0, true)
	fixtureThemeImage(t, filepath.Join(stills, "intros", "nested.png"), 0, false)
	fixtureFile(t, filepath.Join(stills, "loop.mp4"), []byte("native video"))
	fixtureFile(t, filepath.Join(stills, "broken.png"), []byte("not an image"))
	for i := 0; i < 98; i++ {
		fixtureThemeImage(t, filepath.Join(stills, fmt.Sprintf("%02d-bundled.png", i)), byte(i+1), false)
	}

	config, err := s.Configuration(t.Context())
	if err != nil || !config.OmarchyThemeAvailable {
		t.Fatalf("expected active theme availability: %+v %v", config, err)
	}
	config, err = s.EditOmarchyTheme(t.Context(), "add", EditOmarchyThemeRequest{Revision: config.Revision})
	if err != nil || config.OmarchyTheme == nil || config.OmarchyTheme.ImageRules != nil || backend.calls.Load() != 0 {
		t.Fatalf("adding did not save a default inherited source: %+v %v", config, err)
	}
	id := config.OmarchyTheme.ID
	collectFixtureTheme(t, s, id)
	preview, err := s.OmarchyThemePreview(t.Context(), 1, 4)
	if err != nil || preview.Theme != "Ethereal" || preview.Eligible == nil || *preview.Eligible != 0 || len(preview.Stills) != 4 || preview.Total != 100 {
		t.Fatalf("shared summary: %+v %v", preview, err)
	}
	if preview.Stills[0].Width != 64 || preview.Stills[0].Height != 36 || len(preview.Stills[0].JPEG) == 0 || preview.Palette[0].Color != "#7e83dc" {
		t.Fatal("gallery did not expose decoded geometry, thumbnail or active colors")
	}
	last, err := s.OmarchyThemePreview(t.Context(), 9, 12)
	if err != nil || last.Page != 9 || len(last.Stills) != 4 || last.Revision != preview.Revision {
		t.Fatalf("unexpected page: page=%d stills=%d err=%v", last.Page, len(last.Stills), err)
	}
	previewFiles, err := os.ReadDir(s.request.CachePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range previewFiles {
		if filepath.Ext(file.Name()) == ".jpg" || filepath.Ext(file.Name()) == ".png" || filepath.Ext(file.Name()) == ".webp" {
			t.Fatal("gallery generated a playback-cache image")
		}
	}
	custom := SourceImageRules{MinWidth: 1, MinHeight: 1, Ratio: "any"}
	rules, err := s.PreviewRules(t.Context(), RulePreviewRequest{ProviderID: omarchyProvider, InputID: id, Revision: config.Revision, Rules: ImageRules{MinWidth: 1, MinHeight: 1, Ratio: "any"}})
	if err != nil || rules.Matched != 100 || rules.Validation != "discovery_decoded_dimensions" {
		t.Fatalf("draft geometry did not use discovered dimensions: %+v %v", rules, err)
	}
	config, err = s.EditOmarchyTheme(t.Context(), "update", EditOmarchyThemeRequest{Revision: config.Revision, ID: id, ImageRules: &custom})
	if err != nil || config.OmarchyTheme == nil || config.OmarchyTheme.ImageRules == nil || config.Rules.MinWidth != 5120 {
		t.Fatalf("override changed shared rules or was not saved: %+v %v", config, err)
	}
	status, err := s.Action(t.Context(), "next")
	if err != nil || status.Current.SourceID != id || filepath.Ext(status.Current.Path) != ".webp" || backend.calls.Load() != 1 {
		t.Fatalf("expected static WebP through normal playback: %+v %v", status, err)
	}
	accepted, err := s.Preview(t.Context(), status.Current.ImageID)
	if err != nil || !accepted.Available || accepted.MIMEType != "image/png" {
		t.Fatalf("WebP cache preview failed: %+v %v", accepted, err)
	}
	if _, err := s.Action(t.Context(), "exclude"); err != nil {
		t.Fatal(err)
	}
	status, err = s.Action(t.Context(), "next")
	if err != nil || filepath.Ext(status.Current.Path) != ".jpg" || backend.calls.Load() != 2 {
		t.Fatalf("exclusion did not skip the WebP: %+v %v", status, err)
	}
	inherit := true
	config, err = s.EditOmarchyTheme(t.Context(), "update", EditOmarchyThemeRequest{Revision: config.Revision, ID: id, InheritRules: &inherit})
	if err != nil || config.OmarchyTheme.ImageRules != nil {
		t.Fatalf("could not restore shared image rules: %+v %v", config, err)
	}
	status, err = s.Action(t.Context(), "next")
	if err == nil || backend.calls.Load() != 2 || status.HistoryLength != 2 {
		t.Fatalf("no eligible still should leave the wallpaper in place: %+v %v", status, err)
	}
	config, err = s.EditOmarchyTheme(t.Context(), "remove", EditOmarchyThemeRequest{Revision: config.Revision, ID: id})
	if err != nil || config.OmarchyTheme != nil || s.Status().HistoryLength != 2 {
		t.Fatalf("removing source changed history or failed: %+v %v", config, err)
	}
	if _, err := os.Stat(filepath.Join(userStills, "00-user.webp")); err != nil {
		t.Fatal(err)
	}
}

func TestOmarchyAutomaticHandoffRejectsAnOldGeneration(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Omarchy is a Linux source")
	}
	s, _, backend := configurationFixture(t, `version=1
[omarchy_theme]
id="theme"
enabled=true
`)
	s.theme.home = t.TempDir()
	root := setFixtureTheme(t, s.theme.home, "ethereal", "a1")
	fixtureWebP(t, filepath.Join(root, "backgrounds", "still.webp"), fixtureStaticWebP)
	collectFixtureTheme(t, s, "theme")
	active, err := omarchytheme.ReadActive(s.theme.home)
	if err != nil {
		t.Fatal(err)
	}
	for _, settled := range []time.Time{{}, time.Now()} {
		s.theme.observation.Store(&themeObservation{active: active, enabled: true, settled: settled})
		_, failure := s.beginThemeOperation(t.Context(), true)
		if failure == nil || failure.Code != "busy" {
			t.Fatalf("automatic playback ran before the full opening interval: %v", failure)
		}
	}
	s.theme.observation.Store(&themeObservation{active: active, enabled: true, settled: time.Now().Add(-s.interval)})
	operation, failure := s.beginThemeOperation(t.Context(), true)
	if failure != nil {
		t.Fatal(failure)
	}
	config, _, err := LoadConfig(s.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	choice, failure := s.selectFresh(operation, config, func(string) {})
	if failure != nil {
		t.Fatal(failure)
	}
	setFixtureTheme(t, s.theme.home, "tokyo-night", "b1")
	setFixtureTheme(t, s.theme.home, "ethereal", "a2")
	if failure := s.applyChoice(operation, choice); failure == nil || failure.Code != "theme_changed" || backend.calls.Load() != 0 || s.state.Pending != nil {
		t.Fatalf("obsolete A→B→A choice reached playback: failure=%v calls=%d pending=%+v", failure, backend.calls.Load(), s.state.Pending)
	}
	if failure := s.applyDesktop(t.Context(), definiteThemeRejection{}, choice, false); failure == nil || failure.Code != "theme_changed" || s.state.Pending != nil {
		t.Fatalf("definite rejection left pending intent: failure=%v intent=%+v", failure, s.state.Pending)
	}
}

type definiteThemeRejection struct{}

func (definiteThemeRejection) Probe(context.Context) error { return nil }
func (definiteThemeRejection) SetWallpaper(context.Context, string, desktop.DisplayOptions) error {
	return fmt.Errorf("native guard: %w", desktop.ErrOmarchyThemeChanged)
}

func TestOmarchyCollectionAPIRejectsLargePages(t *testing.T) {
	s, _, _ := configurationFixture(t, "version=1\n")
	_, err := s.OmarchyThemePreview(t.Context(), 1, 13)
	var failure *Error
	if !errors.As(err, &failure) || failure.Code != "invalid_request" {
		t.Fatalf("unbounded preview accepted: %v", err)
	}
}

func TestOmarchyDiscoveryFollowsThemeWhilePaused(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Omarchy is a Linux source")
	}
	s, _, backend := configurationFixture(t, `version=1
[omarchy_theme]
id="following-theme"
enabled=true
`)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	s.theme.home = t.TempDir()
	commands := t.TempDir()
	shell := filepath.Join(commands, "omarchy-shell")
	fixtureFile(t, shell, []byte("#!/bin/sh\n[ \"$2\" = themeIntroStatus ] && printf ready\n"))
	if err := os.Chmod(shell, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", commands+string(os.PathListSeparator)+os.Getenv("PATH"))
	root := setFixtureTheme(t, s.theme.home, "ethereal", "a1")
	fixtureWebP(t, filepath.Join(root, "backgrounds", "one.webp"), fixtureStaticWebP)
	s.StartDiscovery()
	await := func(name string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
		defer cancel()
		tick := time.NewTicker(15 * time.Millisecond)
		defer tick.Stop()
		for {
			preview, err := s.OmarchyThemePreview(ctx, 1, 4)
			if err == nil && preview.Theme == name && preview.State == "ready" && preview.Total == 1 {
				if s.Status().OmarchyThemeName != name || !s.Status().Paused || backend.calls.Load() != 0 {
					t.Fatalf("discovery changed paused state: %+v", s.Status())
				}
				return
			}
			select {
			case <-ctx.Done():
				t.Fatalf("theme %s did not arrive: %+v %v", name, preview, err)
			case <-tick.C:
			}
		}
	}
	await("Ethereal")
	setFixtureTheme(t, s.theme.home, "tokyo-night", "b1")
	await("Tokyo Night")
	s.Stop()
	setFixtureTheme(t, s.theme.home, "ethereal", "a2")
	if s.Status().OmarchyThemeName != "Tokyo Night" || !s.Status().Paused || backend.calls.Load() != 0 {
		t.Fatal("the stopped service reacted to a native theme change")
	}
}

func TestOmarchyThemeSharesTheExistingSelectionPool(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Omarchy is a Linux source")
	}
	dir := t.TempDir()
	fixtureThemeImage(t, filepath.Join(dir, "folder.png"), 140, false)
	s, _, _ := configurationFixture(t, fmt.Sprintf(`version=1
[omarchy_theme]
id="theme"
enabled=true
[[providers]]
id="local"
enabled=true
[[providers.folders]]
id="folder"
path=%q
[[providers]]
id="wallhaven"
enabled=true
[[providers.searches]]
id="search"
query="nature"
`, dir))
	s.theme.home = t.TempDir()
	root := setFixtureTheme(t, s.theme.home, "ethereal", "a1")
	fixtureWebP(t, filepath.Join(root, "backgrounds", "one.webp"), fixtureStaticWebP)
	collectFixtureTheme(t, s, "theme")
	config, _, err := LoadConfig(s.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	local := config.Providers[0]
	input := providerInputs(local, config.Filters.MaxDepth, sourceDefaultRules(config))[0]
	if _, err := s.refreshInputCounts(t.Context(), local, input, 1); err != nil {
		t.Fatal(err)
	}
	remote := config.Providers[1]
	if _, err := s.store.EnsureImage(t.Context(), remote.ID, "search", config.InputRevision(remote.ID, "search"), "", metadataCandidate("wallhaven-1")); err != nil {
		t.Fatal(err)
	}
	pools, err := s.candidatePools(t.Context(), config, maxCatalogCandidates, func(string) {})
	if err != nil || len(pools) != 3 {
		t.Fatalf("expected local, wallhaven and Omarchy in one pool: %+v %v", pools, err)
	}
	for index, want := range []string{"local", "wallhaven", "omarchy"} {
		if pools[index].id != want || len(pools[index].inputs) != 1 || len(pools[index].inputs[0]) != 1 {
			t.Fatalf("unexpected candidate membership: %+v", pools[index])
		}
	}
}

func TestOmarchyMissingStateCannotEscapeTheMixedRotationGuard(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Omarchy is a Linux source")
	}
	s, _, _ := configurationFixture(t, `version=1
[omarchy_theme]
id="active"
enabled=true
`)
	s.theme.home = t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	release, err := desktop.TryOmarchyThemeLock()
	if err != nil {
		t.Fatal(err)
	}
	_, busy := s.beginThemeOperation(t.Context(), true)
	release()
	if busy == nil || busy.Code != "busy" {
		t.Fatalf("automatic playback proceeded during a native change: %v", busy)
	}

	ctx, failure := s.beginThemeOperation(t.Context(), true)
	if failure != nil {
		t.Fatal(failure)
	}
	choice := playbackChoice{identity: catalog.Identity{ProviderID: "local", InputID: "folder"}}
	_, guard, failure := s.guardThemeChoice(ctx, choice)
	if failure != nil || guard == nil || !guard() {
		t.Fatalf("a truly missing theme must let other inputs rotate: %v", failure)
	}
	setFixtureTheme(t, s.theme.home, "ethereal", "new")
	if guard() {
		t.Fatal("mixed selection could replace the native opening when a theme appeared mid-selection")
	}
}
