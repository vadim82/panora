package playback

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/source"
)

func TestShuffleDrawGivesEachDistinctImageOneInitialOutcome(t *testing.T) {
	local := fairnessPool("local", "home", 1)
	local[0].Hash = "known-local-content"
	remote := fairnessPool("wallhaven", "nature", 99)
	for i := range remote {
		remote[i].Identity.ImageID++
	}
	overlap := slices.Clone(remote[:10])
	for i := range overlap {
		overlap[i].Identity.InputID = "mountains"
	}
	alias := catalog.Candidate{Identity: catalog.Identity{ProviderID: "wallhaven", InputID: "mountains", ImageID: 101}, Hash: "known-local-content"}
	overlap = append(overlap, alias)
	pools := []providerPool{{id: "local", inputs: []inputPool{local}}, {id: "wallhaven", inputs: []inputPool{remote, overlap}}}
	picked := map[int64]bool{}
	for index := range 100 {
		draws := 0
		next := shuffleDraw(pools, func(n int) int {
			draws++
			if draws == 1 {
				if n != 100 {
					t.Fatalf("initial draw has %d distinct images, want 100", n)
				}
				return index
			}
			return 0
		})
		candidate, ok := next()
		if !ok || picked[candidate.Identity.ImageID] {
			t.Fatalf("outcome %d repeated an image or returned no image: %+v", index, candidate)
		}
		picked[candidate.Identity.ImageID] = true
	}
	if !picked[1] || picked[101] {
		t.Fatalf("local image and its known alias have more than one outcome: %v", picked)
	}
}

func TestShuffleDrawRetriesAlternateMembershipAfterOtherImages(t *testing.T) {
	first := fairnessPool("wallhaven", "first", 2)
	overlap := slices.Clone(first[:1])
	overlap[0].Identity.InputID = "overlap"
	next := shuffleDraw([]providerPool{{id: "wallhaven", inputs: []inputPool{first, overlap}}}, func(int) int { return 0 })
	for _, want := range []struct {
		id    int64
		input string
	}{{1, "first"}, {2, "first"}, {1, "overlap"}} {
		candidate, ok := next()
		if !ok || candidate.Identity.ImageID != want.id || candidate.Identity.InputID != want.input {
			t.Fatalf("draw = %+v; want %+v", candidate, want)
		}
	}
	if _, ok := next(); ok {
		t.Fatal("retry drew an exhausted membership")
	}
}

func TestShuffleDrawGivesAnotherProviderATurnAfterFailure(t *testing.T) {
	local := fairnessPool("local", "home", 1)
	remote := fairnessPool("wallhaven", "nature", 99)
	for i := range remote {
		remote[i].Identity.ImageID++
	}
	pools := []providerPool{{id: "local", inputs: []inputPool{local}}, {id: "wallhaven", inputs: []inputPool{remote}}}
	next := shuffleDraw(pools, func(n int) int { return n - 1 })
	first, ok := next()
	if !ok || first.Identity.ProviderID != "wallhaven" {
		t.Fatalf("initial draw did not choose Wallhaven: %+v", first)
	}
	fallback, ok := next()
	if !ok || fallback.Identity.ProviderID != "local" {
		t.Fatalf("shuffle retried Wallhaven before giving the local provider a turn: %+v", fallback)
	}
}

func TestFreshRotationModeWithMixedCatalog(t *testing.T) {
	root := t.TempDir()
	writeScanImage(t, filepath.Join(root, "local.png"), 2, 2)
	writeScanImage(t, filepath.Join(root, "z-local-other-size.png"), 2, 4)
	service, path, backend := configurationFixture(t, localTOML(root)+`
[providers.folders.image_rules]
min_width=1
min_height=1
ratio="any"
exact_resolutions=["2x2"]
[[providers]]
id="wallhaven"
enabled=true
[[providers.searches]]
id="nature"
query="nature"
[[providers.searches]]
id="overlap"
query="mountains"
[[providers.searches]]
id="disabled"
query="disabled"
enabled=false
[[providers.searches]]
id="too-small"
query="too-small"
[providers.searches.image_rules]
min_width=9000
min_height=1
ratio="any"
`)
	service.request.Providers = map[string]source.Provider{"wallhaven": offlineReviewProvider{}}
	config, _, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	p := config.Providers[0]
	input := providerInputs(p, config.Filters.MaxDepth, sourceDefaultRules(config))[0]
	if _, err := service.refreshInputCounts(t.Context(), p, input, 1); err != nil {
		t.Fatal(err)
	}

	seed := func(search, id string) catalog.Identity {
		t.Helper()
		identity, err := service.store.EnsureImage(t.Context(), "wallhaven", search, config.InputRevision("wallhaven", search), "", metadataCandidate(id))
		if err != nil {
			t.Fatal(err)
		}
		return identity
	}
	remotePath := filepath.Join(t.TempDir(), "wallhaven.png")
	writeScanImage(t, remotePath, 2, 3)
	cached, err := service.cache.Materialize(t.Context(), source.Content{Kind: "local", MediaKind: "image", Path: remotePath})
	if err != nil {
		t.Fatal(err)
	}
	saveCached := func(identity catalog.Identity) {
		t.Helper()
		if err := service.store.SaveCachedImage(t.Context(), identity.ImageID, catalog.CachedFile{
			Hash: cached.Hash, Path: cached.Path, Bytes: cached.Bytes, Width: cached.Width, Height: cached.Height,
		}); err != nil {
			t.Fatal(err)
		}
	}
	firstRemote := seed("nature", "wall-0")
	saveCached(firstRemote)
	for i := 1; i < 99; i++ {
		seed("nature", fmt.Sprintf("wall-%d", i))
	}
	if seed("overlap", "wall-0").ImageID != firstRemote.ImageID {
		t.Fatal("overlapping Wallhaven searches should share catalog identity")
	}
	saveCached(seed("overlap", "alias"))
	seed("disabled", "wall-0")
	seed("too-small", "wall-0")
	seed("disabled", "disabled-only")
	seed("too-small", "ineligible-only")

	var bounds []int
	service.draw = func(n int) int {
		bounds = append(bounds, n)
		return 0
	}
	// Balance sees both providers but only the local image matching the exact resolution.
	choice, selectionErr := service.selectFresh(t.Context(), config, func(string) {})
	if selectionErr != nil || choice.identity.ProviderID != "local" || !slices.Equal(bounds, []int{2, 1, 1}) {
		t.Fatalf("default balance draw=%+v bounds=%v error=%v", choice, bounds, selectionErr)
	}
	initial := settings(t, service)
	if initial.Saved.RotationMode != rotationBalanceSources || initial.Effective.RotationMode != rotationBalanceSources {
		t.Fatalf("old settings did not default to balance: %+v", initial)
	}
	mode := rotationShuffleAllImages
	setMode := func(revision string, mode *string) (Configuration, error) {
		return service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: revision, RotationMode: mode})
	}
	updated, err := setMode(initial.Revision, &mode)
	if err != nil || updated.Saved.RotationMode != mode || updated.Effective.RotationMode != mode || updated.Revision == initial.Revision {
		t.Fatalf("saved rotation mode=%+v, error=%v", updated, err)
	}
	invalid := ""
	_, err = setMode(updated.Revision, &invalid)
	requireConfigError(t, err, "invalid_settings")
	_, err = setMode(initial.Revision, &mode)
	requireConfigError(t, err, "config_conflict")
	reopened, _, err := LoadConfig(path)
	if err != nil || reopened.Playback.RotationMode != mode {
		t.Fatalf("reopened rotation mode=%q, error=%v", reopened.Playback.RotationMode, err)
	}

	bounds = nil
	first, err := service.Action(t.Context(), "next")
	if err != nil {
		t.Fatalf("initial shuffle bounds=%v warnings=%v provider revisions before=%s,%s after=%s,%s: %v",
			bounds, first.Warnings, config.InputRevision("local", "home"), config.InputRevision("wallhaven", "nature"),
			reopened.InputRevision("local", "home"), reopened.InputRevision("wallhaven", "nature"), err)
	}
	if !slices.Equal(bounds, []int{100}) || first.Current.SourceID != "home" {
		t.Fatalf("initial shuffle bounds=%v status=%+v", bounds, first)
	}
	bounds = nil
	second, err := service.Action(t.Context(), "next")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(bounds, []int{99, 3}) || second.Current.SourceID != "nature" || second.Current.Path != cached.Path {
		t.Fatalf("shuffle while local is current bounds=%v status=%+v", bounds, second)
	}

	// Revisit the local image, then save a new mode with forward history.
	before, err := service.Action(t.Context(), "previous")
	if err != nil {
		t.Fatal(err)
	}
	beforeDeadline := service.nextAt
	balance := rotationBalanceSources
	if _, err := setMode(updated.Revision, &balance); err != nil {
		t.Fatal(err)
	}
	after := service.Status()
	if !reflect.DeepEqual(after.Current, before.Current) || after.HistoryCursor != before.HistoryCursor ||
		after.HistoryLength != before.HistoryLength || after.Paused != before.Paused ||
		service.nextAt != beforeDeadline || backend.calls.Load() != 3 {
		t.Fatalf("saving mode changed playback: before=%+v after=%+v", before, after)
	}
	bounds = nil
	forward, err := service.Action(t.Context(), "next")
	if err != nil {
		t.Fatal(err)
	}
	if len(bounds) != 0 || forward.Current.Path != second.Current.Path || forward.HistoryLength != second.HistoryLength {
		t.Fatalf("forward history drew a fresh image: bounds=%v status=%+v", bounds, forward)
	}
}

func TestShuffleRechecksReplacedLocalOriginalAfterCacheEviction(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "a.png")
	writeScanImage(t, original, 16, 9)
	writeScanImage(t, filepath.Join(root, "b.png"), 4, 4)
	service, _, _ := configurationFixture(t, localTOML(root)+"[playback]\nrotation_mode='shuffle_all_images'\n")
	service.draw = func(int) int { return 0 }
	first := act(t, service, "next")
	second := act(t, service, "next")
	if first.Current.ImageID == second.Current.ImageID || service.currentOriginPath(t.Context()) == original {
		t.Fatalf("unexpected fixture history: first=%+v second=%+v", first.Current, second.Current)
	}
	saved, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{
		Revision: settings(t, service).Revision, CacheMaxImages: configValue(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first.Current.Path); !os.IsNotExist(err) {
		t.Fatalf("old cached copy was not evicted: %v", err)
	}
	writeScanImage(t, original, 6, 4)
	if _, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{
		Revision: saved.Revision, CacheMaxImages: configValue(2), Ratio: configValue("3:2"),
	}); err != nil {
		t.Fatal(err)
	}
	third := act(t, service, "next")
	if third.Current.ImageID != first.Current.ImageID || third.Current.Path == first.Current.Path || third.HistoryLength != 3 {
		t.Fatalf("shuffle did not rematerialize the replaced original: %+v", third)
	}
}
