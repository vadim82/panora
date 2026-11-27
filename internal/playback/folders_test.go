package playback

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/source"
	"github.com/vadim82/panora/internal/sources/local"
)

func TestFolderEditsPreserveIdentityAndInheritedDefaults(t *testing.T) {
	root, other := t.TempDir(), t.TempDir()
	service, path, backend := configurationFixture(t, localTOML(root)+fmt.Sprintf("[[providers.folders]]\nid='other'\npath=%q\n", other))
	before := settings(t, service)
	if !before.Folders[0].Enabled || !before.Folders[0].InputEnabled || before.Folders[0].MaxDepth != nil || before.Folders[0].EffectiveMaxDepth != 2 {
		t.Fatalf("legacy defaults=%+v", before.Folders)
	}
	saved, err := service.UpdateFolder(t.Context(), UpdateFolderRequest{Revision: before.Revision, ID: "home", Name: configValue(" Landscapes "), Enabled: configValue(false), MaxDepth: configValue(0), RefreshInterval: configValue("10s")})
	if err != nil {
		t.Fatal(err)
	}
	f := saved.Folders[0]
	if f.ID != "home" || f.Name != "Landscapes" || f.Enabled || f.InputEnabled || f.MaxDepth == nil || *f.MaxDepth != 0 || f.EffectiveMaxDepth != 0 || f.RefreshInterval != "10s" || !saved.Folders[1].Enabled {
		t.Fatalf("edited=%+v", saved)
	}
	config, _, err := LoadConfig(path)
	if err != nil || !config.HasEnabledInputs() || config.Providers[0].Folders[1].Path != other {
		t.Fatalf("saved=%+v %v", config, err)
	}
	unchanged, err := service.UpdateFolder(t.Context(), UpdateFolderRequest{Revision: saved.Revision, ID: "home", Name: configValue("Landscapes"), Enabled: configValue(false), MaxDepth: configValue(0), RefreshInterval: configValue("10s")})
	if err != nil || unchanged.Revision != saved.Revision || unchanged.TOML != saved.TOML {
		t.Fatalf("no-op=%+v %v", unchanged, err)
	}
	restored, err := service.UpdateFolder(t.Context(), UpdateFolderRequest{Revision: saved.Revision, ID: "home", Name: configValue(""), Enabled: configValue(true), MaxDepth: configValue(-1), RefreshInterval: configValue("")})
	if err != nil || restored.Folders[0].Name != filepath.Base(root) || restored.Folders[0].MaxDepth != nil || restored.Folders[0].EffectiveMaxDepth != 2 || restored.Folders[0].RefreshInterval != "5m0s" {
		t.Fatalf("restored=%+v %v", restored, err)
	}
	unavailable := filepath.Join(t.TempDir(), "unmounted")
	saved, err = service.UpdateFolder(t.Context(), UpdateFolderRequest{Revision: restored.Revision, ID: "home", Path: &unavailable})
	if err != nil || saved.Folders[0].Path != unavailable || saved.Folders[0].ID != "home" {
		t.Fatalf("unavailable save=%+v %v", saved, err)
	}
	service.Close()
	restarted := openTestService(t, service.request, service.resolve, time.Hour, false)
	if got := settings(t, restarted); got.Revision != saved.Revision || got.Folders[0].ID != "home" || got.Folders[0].Path != unavailable {
		t.Fatalf("restart=%+v", got)
	}
	if backend.calls.Load() != 0 || !restarted.Status().Paused {
		t.Fatal("editing changed desktop or pause")
	}
}

func TestFolderEditValidationPreservesSavedBytes(t *testing.T) {
	root, other := t.TempDir(), t.TempDir()
	service, _, _ := configurationFixture(t, localTOML(root)+fmt.Sprintf("[[providers.folders]]\nid='other'\npath=%q\n", other))
	before := settings(t, service)
	for _, request := range []UpdateFolderRequest{
		{ID: "home", Path: configValue("relative")},
		{ID: "home", Path: configValue("/bad\x00path")},
		{ID: "home", Path: &other},
		{ID: "home", Name: configValue(strings.Repeat("x", 257))},
		{ID: "home", Name: configValue("bad\nlabel")},
		{ID: "home", MaxDepth: configValue(-2)},
		{ID: "home", RefreshInterval: configValue("0s")},
	} {
		request.Revision = before.Revision
		_, err := service.UpdateFolder(t.Context(), request)
		requireConfigError(t, err, "invalid_settings")
		if got := settings(t, service); got.TOML != before.TOML {
			t.Fatal("invalid edit changed saved bytes")
		}
	}
	_, err := service.UpdateFolder(t.Context(), UpdateFolderRequest{Revision: before.Revision, ID: "missing", Name: configValue("New")})
	requireConfigError(t, err, "folder_not_found")
	_, err = service.UpdateFolder(t.Context(), UpdateFolderRequest{Revision: before.Revision, ID: "home"})
	requireConfigError(t, err, "invalid_request")
	_, err = service.UpdateFolder(t.Context(), UpdateFolderRequest{Revision: "stale", ID: "home", Enabled: configValue(false)})
	requireConfigError(t, err, "config_conflict")
	_, err = service.RemoveFolder(t.Context(), RemoveFolderRequest{Revision: "stale", ID: "home"})
	requireConfigError(t, err, "config_conflict")
	_, err = service.RemoveFolder(t.Context(), RemoveFolderRequest{Revision: before.Revision, ID: "missing"})
	requireConfigError(t, err, "folder_not_found")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = service.RemoveFolder(ctx, RemoveFolderRequest{Revision: before.Revision, ID: "home"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled=%v", err)
	}
}

type singleImageLocalPage struct{ local.Provider }

func (p singleImageLocalPage) Discover(ctx context.Context, params source.DiscoverParams) (source.Page, error) {
	params.Limit = 1
	return p.Provider.Discover(ctx, params)
}

func TestLocalDiscoveryRestartsAfterEarlierPathChanges(t *testing.T) {
	for _, test := range []struct {
		name          string
		remove        bool
		duringRestart []string
		afterComplete []string
	}{
		{"deletion", true, []string{"b.png", "c.png", "d.png"}, []string{"c.png", "d.png"}},
		{"insertion", false, []string{"a.png", "b.png", "c.png", "d.png"}, []string{"a.png", "b.png", "c.png", "d.png"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, root := testService(t, &fakeWallpaperBackend{})
			if err := os.Rename(filepath.Join(root, "a.png"), filepath.Join(root, "d.png")); err != nil {
				t.Fatal(err)
			}
			service.request.Source = singleImageLocalPage{}
			config, _, err := LoadConfig(service.request.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			p := config.Providers[0]
			input := providerInputs(p, config.Filters.MaxDepth, sourceDefaultRules(config))[0]
			ctx := t.Context()
			requireCandidates := func(want []string) {
				t.Helper()
				rows, err := service.store.Candidates(ctx, p.ID, input.id, input.revision, 10)
				if err != nil {
					t.Fatal(err)
				}
				ids := make([]string, 0, len(rows))
				for _, row := range rows {
					ids = append(ids, row.Candidate.ID)
				}
				slices.Sort(ids)
				if !slices.Equal(ids, want) {
					t.Fatalf("candidates=%v want %v", ids, want)
				}
			}
			if counts, err := service.refreshInputCounts(ctx, p, input, 3); err != nil || counts.Pages != 3 {
				t.Fatalf("initial scan: %+v %v", counts, err)
			}
			if counts, err := service.refreshInputCounts(ctx, p, input, 1); err == nil || counts.Pages != 1 {
				t.Fatalf("refresh did not save a partial scan: %+v %v", counts, err)
			}
			if test.remove {
				if err := os.Remove(filepath.Join(root, "b.png")); err != nil {
					t.Fatal(err)
				}
			} else {
				writeScanImage(t, filepath.Join(root, "a.png"), 2, 2)
			}
			if counts, err := service.refreshInputCounts(ctx, p, input, 2); err == nil || counts.Pages != 1 {
				t.Fatalf("refresh did not restart from the first page: %+v %v", counts, err)
			}
			requireCandidates(test.duringRestart)
			if _, err := service.refreshInputCounts(ctx, p, input, 3); err != nil {
				t.Fatal(err)
			}
			requireCandidates(test.afterComplete)
		})
	}
}

func TestFolderRootEditSeparatesCachedImagesAndRetainsHistory(t *testing.T) {
	oldRoot, newRoot := t.TempDir(), t.TempDir()
	writeScanImage(t, filepath.Join(oldRoot, "same.png"), 2, 2)
	writeScanImage(t, filepath.Join(newRoot, "same.png"), 3, 2)
	service, _, backend := configurationFixture(t, localTOML(oldRoot)+"[filters]\nmin_width=2\nmin_height=2\n")
	refreshForTest(t, service)
	if _, err := service.Action(t.Context(), "next"); err != nil {
		t.Fatal(err)
	}
	oldEntry := service.state.History[0]
	oldCache, ok, err := service.store.CachedImage(t.Context(), oldEntry.Identity.ImageID)
	if err != nil || !ok {
		t.Fatalf("old cache=%+v %v", oldCache, err)
	}
	before := settings(t, service)
	edited, err := service.UpdateFolder(t.Context(), UpdateFolderRequest{Revision: before.Revision, ID: "home", Path: &newRoot})
	if err != nil || edited.Folders[0].ID != "home" {
		t.Fatalf("root edit=%+v %v", edited, err)
	}
	refreshForTest(t, service)
	if _, err := service.Action(t.Context(), "next"); err != nil {
		t.Fatal(err)
	}
	newEntry := service.state.History[1]
	newCache, ok, err := service.store.CachedImage(t.Context(), newEntry.Identity.ImageID)
	if err != nil || !ok || oldEntry.Identity.ImageID == newEntry.Identity.ImageID || newCache.Hash == oldCache.Hash || newCache.Width != 3 {
		t.Fatalf("old=%+v new=%+v oldID=%+v newID=%+v err=%v", oldCache, newCache, oldEntry.Identity, newEntry.Identity, err)
	}
	if _, err := service.Action(t.Context(), "previous"); err == nil || service.state.Cursor != 1 {
		t.Fatal("old-root history replayed under the new root")
	}
	retained, err := service.store.LookupCandidate(t.Context(), oldEntry.Identity)
	if err != nil || retained.Content.Path != filepath.Join(oldRoot, "same.png") {
		t.Fatalf("history origin changed=%+v %v", retained, err)
	}
	service.Close()
	restarted := openTestService(t, service.request, service.resolve, time.Hour, false)
	refreshForTest(t, restarted)
	if _, err := restarted.Action(t.Context(), "next"); err != nil {
		t.Fatal(err)
	}
	if got := restarted.state.History[len(restarted.state.History)-1]; got.Identity.ImageID != newEntry.Identity.ImageID {
		t.Fatalf("restart lost root identity=%+v", got)
	}
	current := settings(t, restarted)
	if _, err := restarted.UpdateFolder(t.Context(), UpdateFolderRequest{Revision: current.Revision, ID: "home", Path: &oldRoot}); err != nil {
		t.Fatal(err)
	}
	refreshForTest(t, restarted)
	if _, err := restarted.Action(t.Context(), "next"); err != nil {
		t.Fatal(err)
	}
	if got := restarted.state.History[restarted.state.Cursor]; got.Identity.ImageID != oldEntry.Identity.ImageID {
		t.Fatalf("return to root lost original identity=%+v", got)
	}
	if backend.calls.Load() != 4 {
		t.Fatalf("desktop calls=%d", backend.calls.Load())
	}
}

func TestFolderDepthDisableAndRemovalRetainOriginalsAndHistory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub", "deep"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"root.png", "sub/one.png", "sub/deep/two.png"} {
		writeScanImage(t, filepath.Join(root, name), 2, 2)
	}
	service, _, backend := configurationFixture(t, localTOML(root)+"[filters]\nmin_width=2\nmin_height=2\n")
	count := func(want int) {
		t.Helper()
		refreshForTest(t, service)
		config, _, err := LoadConfig(service.request.ConfigPath)
		if err != nil {
			t.Fatal(err)
		}
		candidates, err := service.store.Candidates(t.Context(), "local", "home", config.InputRevision("local", "home"), maxCatalogCandidates)
		if err != nil || len(candidates) != want {
			t.Fatalf("candidates=%d want=%d err=%v", len(candidates), want, err)
		}
	}
	count(3)
	if _, err := service.Action(t.Context(), "next"); err != nil {
		t.Fatal(err)
	}
	before := settings(t, service)
	edited, err := service.UpdateFolder(t.Context(), UpdateFolderRequest{Revision: before.Revision, ID: "home", MaxDepth: configValue(0)})
	if err != nil {
		t.Fatal(err)
	}
	count(1)
	edited, err = service.UpdateFolder(t.Context(), UpdateFolderRequest{Revision: edited.Revision, ID: "home", Enabled: configValue(false)})
	if err != nil {
		t.Fatal(err)
	}
	service.StartDiscovery()
	awaitCondition(t, func() bool {
		s := service.Status()
		return s.SetupRequired && len(s.Providers) == 1 && len(s.Providers[0].Inputs) == 1 && s.Providers[0].Inputs[0].State == "disabled"
	})
	if _, err := service.Action(t.Context(), "next"); err == nil {
		t.Fatal("disabled folder still contributes")
	}
	edited, err = service.UpdateFolder(t.Context(), UpdateFolderRequest{Revision: edited.Revision, ID: "home", Enabled: configValue(true), MaxDepth: configValue(-1)})
	if err != nil {
		t.Fatal(err)
	}
	awaitCondition(t, func() bool {
		s := service.Status()
		return !s.SetupRequired && s.Providers[0].Inputs[0].State == "ready"
	})
	removed, err := service.RemoveFolder(t.Context(), RemoveFolderRequest{Revision: edited.Revision, ID: "home"})
	if err != nil || len(removed.Folders) != 0 {
		t.Fatalf("remove=%+v %v", removed, err)
	}
	awaitCondition(t, func() bool { return service.Status().SetupRequired })
	if service.Status().HistoryLength != 1 || backend.calls.Load() != 1 || !service.Status().Paused {
		t.Fatal("removal changed history, desktop or pause")
	}
	for _, name := range []string{"root.png", "sub/one.png", "sub/deep/two.png"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	service.Close()
	restarted := openTestService(t, service.request, service.resolve, time.Hour, false)
	if !restarted.Status().SetupRequired || restarted.Status().HistoryLength != 1 || len(settings(t, restarted).Folders) != 0 {
		t.Fatalf("restart=%+v", restarted.Status())
	}
}

func TestFolderRemovalCancelsAndRejectsInFlightPage(t *testing.T) {
	service, _ := testService(t, &fakeWallpaperBackend{})
	config, _, err := LoadConfig(service.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	started, canceled := make(chan struct{}), make(chan struct{})
	released, release := context.WithCancel(t.Context())
	t.Cleanup(release)
	service.request.Source = changedProvider{listing: func(ctx context.Context, p source.DiscoverParams) (source.Page, error) {
		page, err := (local.Provider{}).Discover(ctx, p)
		close(started)
		<-ctx.Done()
		close(canceled)
		<-released.Done()
		// Ignore cancellation deliberately: the host must reject obsolete results.
		return page, err
	}}
	service.StartDiscovery()
	<-started
	before := settings(t, service)
	_, err = service.RemoveFolder(t.Context(), RemoveFolderRequest{Revision: before.Revision, ID: "home"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("obsolete job was not canceled")
	}
	release()
	awaitCondition(t, func() bool { return !service.Status().Refreshing })
	p := config.Providers[0]
	candidates, err := service.store.Candidates(t.Context(), p.ID, "home", config.InputRevision(p.ID, "home"), maxCatalogCandidates)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("obsolete page committed=%+v %v", candidates, err)
	}
	scan, err := service.store.StartScan(t.Context(), p.ID, "home", config.InputRevision(p.ID, "home"), false)
	if err != nil {
		t.Fatal(err)
	}
	err = service.commitDiscoveryPage(t.Context(), scan, "home", source.Page{Candidates: []source.Candidate{}, NextCursor: []byte("null")})
	if !errors.Is(err, catalog.ErrStaleScan) {
		t.Fatalf("guard accepted removed input=%v", err)
	}
}

func TestFolderEditDoesNotEnableDisabledProvider(t *testing.T) {
	service, _, _ := configurationFixture(t, strings.Replace(localTOML(t.TempDir()), "enabled = true", "enabled = false", 1))
	before := settings(t, service)
	saved, err := service.UpdateFolder(t.Context(), UpdateFolderRequest{Revision: before.Revision, ID: "home", Enabled: configValue(true), Name: configValue("Saved")})
	if err != nil || saved.Folders[0].Enabled || !saved.Folders[0].InputEnabled || !service.Status().SetupRequired {
		t.Fatalf("disabled provider changed=%+v %v", saved, err)
	}
}

func TestLegacyLocalHistoryAndExclusionsSurviveRootQualifiedDiscovery(t *testing.T) {
	root := t.TempDir()
	writeScanImage(t, filepath.Join(root, "image.png"), 2, 2)
	service, _, backend := configurationFixture(t, localTOML(root)+"[filters]\nmin_width=2\nmin_height=2\n")
	config, _, err := LoadConfig(service.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	p := config.Providers[0]
	input := providerInputs(p, config.Filters.MaxDepth, sourceDefaultRules(config))[0]
	// Simulate catalog entries recorded before roots were part of image scope.
	if err := discoverMetadata(t.Context(), service.store, local.Provider{}, p.ID, input.id, input.revision, input.id, input.config); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Action(t.Context(), "next"); err != nil {
		t.Fatal(err)
	}
	original := service.state.History[0]
	if _, err := service.Action(t.Context(), "exclude"); err != nil {
		t.Fatal(err)
	}
	refreshForTest(t, service)
	if _, err := service.Action(t.Context(), "next"); err == nil || backend.calls.Load() != 1 {
		t.Fatal("new scope bypassed an existing content exclusion")
	}
	page, err := service.Exclusions(t.Context(), 0, 10)
	if err != nil || len(page.Entries) != 1 || page.Entries[0].SourceID != "home" {
		t.Fatalf("legacy exclusion=%+v %v", page, err)
	}
	if _, err := service.RestoreExclusion(t.Context(), page.Entries[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.materializeChoice(t.Context(), playbackChoice{identity: original.Identity, historyIndex: 0}, config); err != nil {
		t.Fatalf("legacy history became unusable=%v", err)
	}
	if service.state.History[0].Identity != original.Identity {
		t.Fatal("legacy history rewritten")
	}
}

func TestFolderAdditionAcceptsSavedOptionsAndValidatesThem(t *testing.T) {
	service, _, _ := configurationFixture(t, "version=1\n")
	before := settings(t, service)
	saved, err := service.AddFolder(t.Context(), AddFolderRequest{Revision: before.Revision, Path: t.TempDir(), Name: "Offline", Enabled: configValue(false), MaxDepth: configValue(0), RefreshInterval: "10s"})
	if err != nil || saved.Folders[0].Name != "Offline" || saved.Folders[0].Enabled || saved.Folders[0].InputEnabled || saved.Folders[0].EffectiveMaxDepth != 0 || saved.Folders[0].RefreshInterval != "10s" {
		t.Fatalf("options=%+v %v", saved, err)
	}
	for _, request := range []AddFolderRequest{
		{Path: t.TempDir(), MaxDepth: configValue(-1)},
		{Path: t.TempDir(), Name: strings.Repeat("x", 257)},
		{Path: t.TempDir(), RefreshInterval: "0s"},
	} {
		request.Revision = saved.Revision
		_, err := service.AddFolder(t.Context(), request)
		requireConfigError(t, err, "invalid_settings")
		if settings(t, service).TOML != saved.TOML {
			t.Fatal("invalid addition changed configuration")
		}
	}
}
