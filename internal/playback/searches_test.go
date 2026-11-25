package playback

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/source"
)

func TestSearchSettingsCompatibilityValidationAndRevision(t *testing.T) {
	base := "version=1\n[[providers]]\nid='wallhaven'\nenabled=true\n[[providers.searches]]\nid='nature'\nquery='nature'\n"
	config, _, err := ParseConfig([]byte(base))
	if err != nil {
		t.Fatal(err)
	}
	search := config.Providers[0].Searches[0]
	if !config.HasEnabledInputs() || !search.enabled() || search.label() != "nature" ||
		configuredSearches(config)[0].TagMatch != source.WallhavenTagMatchAll {
		t.Fatalf("legacy defaults=%+v", search)
	}
	raw := providerInputs(config.Providers[0], 2, sourceDefaultRules(config))[0]
	var wire source.WallhavenConfig
	if err := json.Unmarshal(raw.config, &wire); err != nil || wire.Categories != "100" || wire.Sorting != "relevance" || wire.MaxPages != 20 || wire.Revision != raw.revision || raw.scope != "" {
		t.Fatalf("provider defaults=%+v input=%+v %v", wire, raw, err)
	}
	for _, bad := range []string{"categories='xxx'", "sorting='bad'", "order='bad'", "max_pages=0", "max_pages=101", "top_range='bad'", "query='second'", "purity='111'", "apikey='secret'"} {
		if _, _, err := ParseConfig([]byte(base + bad)); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	for _, query := range []string{strings.Repeat("x", 2049), "bad\nquery", "bad\x00query"} {
		service, _, _ := configurationFixture(t, "version=1\n")
		_, err := service.AddSearch(t.Context(), AddSearchRequest{Revision: settings(t, service).Revision, Name: "Invalid", Query: query})
		requireConfigError(t, err, "invalid_settings")
	}
	for _, suffix := range []string{"name='New label'", "refresh_interval='10s'", "enabled=false"} {
		changed, _, err := ParseConfig([]byte(base + suffix))
		if err != nil || changed.InputRevision("wallhaven", "nature") != raw.revision {
			t.Fatalf("label/schedule changed discovery revision: %s %v", suffix, err)
		}
	}
	for _, suffix := range []string{"categories='110'", "sorting='random'", "max_pages=1"} {
		changed, _, err := ParseConfig([]byte(base + suffix))
		if err != nil || changed.InputRevision("wallhaven", "nature") == raw.revision {
			t.Fatalf("search edit reused discovery revision: %s %v", suffix, err)
		}
	}
}

func TestSearchEditsPreserveStableIdentityAndRestart(t *testing.T) {
	service, path, backend := configurationFixture(t, "version=1\n")
	initial := settings(t, service)
	added, err := service.AddSearch(t.Context(), AddSearchRequest{Revision: initial.Revision, Name: " Landscapes ", Query: "nature", Categories: "110", Sorting: "toplist", Order: "asc", MaxPages: configValue(3), TopRange: "1w", RefreshInterval: "10s"})
	if err != nil {
		t.Fatal(err)
	}
	first := added.Searches[0]
	if first.ID == first.Name || first.Name != "Landscapes" || !first.Enabled || first.Categories != "110" || first.Sorting != "toplist" || first.Order != "asc" || first.MaxPages != 3 || first.TopRange != "1w" || first.Purity != "sfw" || first.RefreshInterval != "10s" {
		t.Fatalf("saved=%+v", first)
	}
	// Equal queries/names remain separate inputs, with shared image identity later.
	added, err = service.AddSearch(t.Context(), AddSearchRequest{Revision: added.Revision, Name: "Landscapes", Query: "nature"})
	if err != nil || len(added.Searches) != 2 || added.Searches[0].ID == added.Searches[1].ID {
		t.Fatalf("multiple=%+v %v", added, err)
	}
	edited, err := service.UpdateSearch(t.Context(), UpdateSearchRequest{Revision: added.Revision, ID: first.ID, Name: configValue("Night skies"), Query: configValue("stars"), Enabled: configValue(false), Categories: configValue("100"), Sorting: configValue("date_added"), Order: configValue("desc"), MaxPages: configValue(1), TopRange: configValue("1M"), RefreshInterval: configValue("")})
	if err != nil || edited.Searches[0].ID != first.ID || edited.Searches[0].Name != "Night skies" || edited.Searches[0].Query != "stars" || edited.Searches[0].Enabled || edited.Searches[0].InputEnabled || edited.Searches[0].RefreshInterval != "5m0s" || edited.Searches[1].ID != added.Searches[1].ID {
		t.Fatalf("edit=%+v %v", edited, err)
	}
	unchanged, err := service.UpdateSearch(t.Context(), UpdateSearchRequest{Revision: edited.Revision, ID: first.ID, Enabled: configValue(false), Query: configValue("stars")})
	if err != nil || unchanged.Revision != edited.Revision || unchanged.TOML != edited.TOML {
		t.Fatalf("no-op=%+v %v", unchanged, err)
	}
	restored, err := service.UpdateSearch(t.Context(), UpdateSearchRequest{Revision: edited.Revision, ID: first.ID, Enabled: configValue(true)})
	if err != nil || !restored.Searches[0].Enabled {
		t.Fatalf("reenable=%+v %v", restored, err)
	}
	saved, _, err := LoadConfig(path)
	if err != nil || saved.Providers[0].Searches[0].ID != first.ID {
		t.Fatalf("TOML=%+v %v", saved, err)
	}
	service.Close()
	restarted := openTestService(t, service.request, service.resolve, time.Hour, false)
	if got := settings(t, restarted); got.Revision != restored.Revision || !reflect.DeepEqual(got.Searches, restored.Searches) {
		t.Fatalf("restart=%+v", got)
	}
	if backend.calls.Load() != 0 || !restarted.Status().Paused {
		t.Fatal("search save changed wallpaper or pause")
	}
}

func TestSearchInvalidEditsConflictsAndDisabledProvider(t *testing.T) {
	service, _, _ := configurationFixture(t, "version=1\n")
	initial := settings(t, service)
	added, err := service.AddSearch(t.Context(), AddSearchRequest{Revision: initial.Revision, Name: "Nature", Query: "nature"})
	if err != nil {
		t.Fatal(err)
	}
	id := added.Searches[0].ID
	for _, edit := range []UpdateSearchRequest{
		{ID: id, TagMatch: configValue("either")}, {ID: id, Query: configValue("bad\nquery")}, {ID: id, Categories: configValue("1000")}, {ID: id, Sorting: configValue("bad")}, {ID: id, Order: configValue("bad")}, {ID: id, MaxPages: configValue(0)}, {ID: id, TopRange: configValue("bad")}, {ID: id, RefreshInterval: configValue("0s")}, {ID: id, Name: configValue("bad\nname")},
	} {
		edit.Revision = added.Revision
		_, err := service.UpdateSearch(t.Context(), edit)
		requireConfigError(t, err, "invalid_settings")
		if settings(t, service).TOML != added.TOML {
			t.Fatal("invalid edit changed saved bytes")
		}
	}
	_, err = service.UpdateSearch(t.Context(), UpdateSearchRequest{Revision: added.Revision, ID: id})
	requireConfigError(t, err, "invalid_request")
	_, err = service.UpdateSearch(t.Context(), UpdateSearchRequest{Revision: initial.Revision, ID: id, Enabled: configValue(false)})
	requireConfigError(t, err, "config_conflict")
	_, err = service.RemoveSearch(t.Context(), RemoveSearchRequest{Revision: initial.Revision, ID: id})
	requireConfigError(t, err, "config_conflict")
	_, err = service.RemoveSearch(t.Context(), RemoveSearchRequest{Revision: added.Revision, ID: "missing"})
	requireConfigError(t, err, "search_not_found")
	optional, err := service.AddSearch(t.Context(), AddSearchRequest{Revision: added.Revision, Query: "query without a custom label"})
	if err != nil || optional.Searches[1].Name != "query without a custom label" {
		t.Fatalf("query label=%+v %v", optional, err)
	}
	disabled, _, _ := configurationFixture(t, "version=1\n[[providers]]\nid='wallhaven'\nenabled=false\n[[providers.searches]]\nid='saved'\nquery='nature'\n")
	before := settings(t, disabled)
	_, err = disabled.AddSearch(t.Context(), AddSearchRequest{Revision: before.Revision, Name: "New", Query: "stars"})
	requireConfigError(t, err, "provider_disabled")
	saved, err := disabled.UpdateSearch(t.Context(), UpdateSearchRequest{Revision: before.Revision, ID: "saved", Enabled: configValue(true), Name: configValue("Saved")})
	if err != nil || saved.Searches[0].Enabled || !saved.Searches[0].InputEnabled || !disabled.Status().SetupRequired {
		t.Fatalf("provider gate=%+v %v", saved, err)
	}
}

func TestUnavailableSearchSaveRemainsUntilVerifiedByDiscovery(t *testing.T) {
	service, _, backend := configurationFixture(t, "version=1\n")
	service.request.Providers = map[string]source.Provider{"wallhaven": offlineReviewProvider{}}
	before := settings(t, service)
	saved, err := service.AddSearch(t.Context(), AddSearchRequest{Revision: before.Revision, Name: "Offline", Query: "nature"})
	if err != nil {
		t.Fatal(err)
	}
	if service.Status().Refreshing || len(service.Status().Providers) != 0 {
		t.Fatal("save performed provider verification")
	}
	service.StartDiscovery()
	awaitCondition(t, func() bool {
		s := service.Status()
		return !s.SetupRequired && len(s.Providers) == 1 && len(s.Providers[0].Inputs) == 1 && s.Providers[0].Inputs[0].Failure != nil
	})
	if got := settings(t, service); got.Revision != saved.Revision || got.Searches[0].Query != "nature" || backend.calls.Load() != 0 {
		t.Fatalf("outage discarded choice=%+v", got)
	}
}

func TestSearchChangesCancelAndRejectInFlightPages(t *testing.T) {
	for _, operation := range []string{"edit", "disable", "remove"} {
		t.Run(operation, func(t *testing.T) {
			service, _, _ := configurationFixture(t, "version=1\n")
			saved, err := service.AddSearch(t.Context(), AddSearchRequest{Revision: settings(t, service).Revision, Name: "Nature", Query: "nature"})
			if err != nil {
				t.Fatal(err)
			}
			config, _, err := LoadConfig(service.request.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			old := config.Providers[0]
			oldRevision := config.InputRevision(old.ID, saved.Searches[0].ID)
			started, canceled := make(chan struct{}), make(chan struct{})
			released, release := context.WithCancel(t.Context())
			t.Cleanup(release)
			var calls atomic.Int32
			service.request.Providers = map[string]source.Provider{"wallhaven": metadataProvider{changedProvider{listing: func(ctx context.Context, _ source.DiscoverParams) (source.Page, error) {
				if calls.Add(1) == 1 {
					close(started)
					<-ctx.Done()
					close(canceled)
					<-released.Done()
					return source.Page{Candidates: []source.Candidate{metadataCandidate("shared")}, NextCursor: json.RawMessage("null")}, nil
				}
				return source.Page{Candidates: []source.Candidate{}, NextCursor: json.RawMessage("null")}, nil
			}}}}
			service.StartDiscovery()
			<-started
			switch operation {
			case "edit":
				_, err = service.UpdateSearch(t.Context(), UpdateSearchRequest{Revision: saved.Revision, ID: saved.Searches[0].ID, Query: configValue("stars")})
			case "disable":
				_, err = service.UpdateSearch(t.Context(), UpdateSearchRequest{Revision: saved.Revision, ID: saved.Searches[0].ID, Enabled: configValue(false)})
			case "remove":
				_, err = service.RemoveSearch(t.Context(), RemoveSearchRequest{Revision: saved.Revision, ID: saved.Searches[0].ID})
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-canceled:
			case <-time.After(2 * time.Second):
				t.Fatal("obsolete search job was not canceled")
			}
			release()
			awaitCondition(t, func() bool { return !service.Status().Refreshing })
			candidates, err := service.store.Candidates(t.Context(), old.ID, saved.Searches[0].ID, oldRevision, maxCatalogCandidates)
			if err != nil || len(candidates) != 0 {
				t.Fatalf("obsolete page committed=%+v %v", candidates, err)
			}
			scan, err := service.store.StartScan(t.Context(), old.ID, saved.Searches[0].ID, oldRevision, false)
			if err != nil {
				t.Fatal(err)
			}
			err = service.commitDiscoveryPage(t.Context(), scan, "", source.Page{Candidates: []source.Candidate{}, NextCursor: json.RawMessage("null")})
			if !errors.Is(err, catalog.ErrStaleScan) {
				t.Fatalf("stale guard=%v", err)
			}
		})
	}
}

func TestSearchRemovalKeepsHistoryAndSharedCachedIdentity(t *testing.T) {
	service, _, backend := configurationFixture(t, "version=1\n[filters]\nmin_width=2\nmin_height=2\n")
	before := settings(t, service)
	saved, err := service.AddSearch(t.Context(), AddSearchRequest{Revision: before.Revision, Name: "First", Query: "nature"})
	if err != nil {
		t.Fatal(err)
	}
	saved, err = service.AddSearch(t.Context(), AddSearchRequest{Revision: saved.Revision, Name: "Second", Query: "nature"})
	if err != nil {
		t.Fatal(err)
	}
	config, _, err := LoadConfig(service.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	original := filepath.Join(root, "image.png")
	writeScanImage(t, original, 2, 2)
	cached, err := service.cache.Materialize(t.Context(), source.Content{MediaKind: "image", Kind: "local", Path: original})
	if err != nil {
		t.Fatal(err)
	}
	candidate := metadataCandidate("shared")
	var first catalog.Identity
	enroll := func(config Config, input string) catalog.Identity {
		t.Helper()
		id, err := service.store.EnsureImage(t.Context(), "wallhaven", input, config.InputRevision("wallhaven", input), "", candidate)
		if err != nil {
			t.Fatal(err)
		}
		if err := service.store.SaveCachedImage(t.Context(), id.ImageID, catalog.CachedFile{Hash: cached.Hash, Path: cached.Path, Bytes: cached.Bytes, Width: cached.Width, Height: cached.Height}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	first = enroll(config, saved.Searches[0].ID)
	second := enroll(config, saved.Searches[1].ID)
	if first.ImageID != second.ImageID {
		t.Fatal("searches duplicated shared image identity")
	}
	service.draw = func(int) int { return 0 }
	if _, err := service.Action(t.Context(), "next"); err != nil {
		t.Fatal(err)
	}
	saved, err = service.RemoveSearch(t.Context(), RemoveSearchRequest{Revision: saved.Revision, ID: saved.Searches[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	config, _, err = LoadConfig(service.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	second = enroll(config, saved.Searches[0].ID)
	if second.ImageID != first.ImageID {
		t.Fatal("removal changed shared image identity")
	}
	if _, err := service.Action(t.Context(), "next"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Action(t.Context(), "previous"); err == nil {
		t.Fatal("removed search history remained playable")
	}
	if service.Status().HistoryLength != 2 || backend.calls.Load() != 2 {
		t.Fatal("removal changed history or wallpaper")
	}
	// Integration with stable history: the shared image stays usable through
	// the remaining search, but an old entry keeps its removed input origin.
	page, err := service.History(t.Context(), "", 10)
	if err != nil || len(page.Entries) != 2 {
		t.Fatalf("history after search removal=%+v %v", page, err)
	}
	if page.Entries[0].ImageID != page.Entries[1].ImageID || page.Entries[0].Availability != "cached" || page.Entries[1].Reason != "outside_enabled_inputs" {
		t.Fatalf("shared image/history availability=%+v", page)
	}
	current := service.Status().Current.ID
	if _, err := service.ReplayHistory(t.Context(), page.Entries[1].ID); err == nil || service.Status().Current.ID != current || backend.calls.Load() != 2 {
		t.Fatalf("removed search replay changed current: %v", err)
	}
	if _, err := service.store.LookupCandidate(t.Context(), first); err != nil {
		t.Fatal("removal deleted original provenance", err)
	}
	if _, err := os.Stat(original); err != nil {
		t.Fatal("removal deleted original", err)
	}
	if _, err := service.RemoveSearch(t.Context(), RemoveSearchRequest{Revision: saved.Revision, ID: saved.Searches[0].ID}); err != nil {
		t.Fatal(err)
	}
	service.Close()
	restarted := openTestService(t, service.request, service.resolve, time.Hour, false)
	if !restarted.Status().SetupRequired || restarted.Status().HistoryLength != 2 || len(settings(t, restarted).Searches) != 0 {
		t.Fatalf("restart=%+v", restarted.Status())
	}
}

func TestFilterOnlySearchSaveAndClear(t *testing.T) {
	service, path, backend := configurationFixture(t, "version=1\n")
	revision := settings(t, service).Revision
	search, err := service.AddSearch(t.Context(), AddSearchRequest{Revision: revision, Enabled: configValue(false)})
	if err != nil || len(search.Searches) != 1 || search.Searches[0].Name != "Wallhaven" || search.Searches[0].Categories != "000" || search.Searches[0].TagMatch != "any" {
		t.Fatalf("empty search: %+v %v", search.Searches, err)
	}
	storedID := search.Searches[0].ID
	rules := &SourceImageRules{MinWidth: 1, MinHeight: 1, ExactResolutions: []string{"1920x1080", "3440x1440"}, Ratio: "presets", RatioPresets: []string{"16:9", "43:18"}}
	filters := UpdateSearchRequest{
		Revision: search.Revision, ID: storedID, Name: configValue("Coast"), ImageRules: rules,
		Tags: configValue([]string{"night sky"}), TagMatch: configValue("all"), Colors: configValue([]string{"#0167cd", "#424153"}),
	}
	filtered, err := service.UpdateSearch(t.Context(), filters)
	if err != nil {
		t.Fatal(err)
	}
	saved, _, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	entry := saved.Providers[0].Searches[0]
	ratio, err := ParseRatioRule(entry.ImageRules.Ratio, entry.ImageRules.RatioPresets)
	if err != nil || entry.ID != storedID || entry.Query != "" || entry.enabled() || entry.TagMatch != "all" || filtered.Searches[0].TagMatch != "all" || !reflect.DeepEqual(entry.Tags, *filters.Tags) || !reflect.DeepEqual(entry.Colors, *filters.Colors) {
		t.Fatalf("persisted: %+v %v", entry, err)
	}
	if !entry.ImageRules.matches(ImageInfo{Width: 1920, Height: 1080}, ratio) ||
		!entry.ImageRules.matches(ImageInfo{Width: 3440, Height: 1440}, ratio) ||
		entry.ImageRules.matches(ImageInfo{Width: 2560, Height: 1440}, ratio) ||
		entry.ImageRules.matches(ImageInfo{Width: 3440, Height: 1441}, ratio) {
		t.Fatal("exact resolution matching did not intersect configured ratios")
	}
	_, err = service.UpdateSearch(t.Context(), filters)
	requireConfigError(t, err, "config_conflict")
	cleared, err := service.UpdateSearch(t.Context(), UpdateSearchRequest{
		Revision: filtered.Revision, ID: storedID, Name: configValue(""), Query: configValue("city"),
		Tags: configValue([]string{}), TagMatch: configValue("any"), Colors: configValue([]string{}), InheritRules: configValue(true),
	})
	if err != nil || len(cleared.Searches) != 1 || cleared.Searches[0].Name != "city" || cleared.Searches[0].CustomName != "" || cleared.Searches[0].ImageRules != nil || len(cleared.Searches[0].Tags) > 0 || cleared.Searches[0].TagMatch != "any" || len(cleared.Searches[0].Colors) > 0 || cleared.Searches[0].ID != storedID || !service.Status().Paused || backend.calls.Load() != 0 {
		t.Fatalf("clearing changed unrelated state or retained filters: %+v %v", cleared.Searches, err)
	}
}
