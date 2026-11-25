package playback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/source"
	"github.com/vadim82/panora/internal/sources/local"
)

func fairnessPool(provider, input string, count int) inputPool {
	out := make(inputPool, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, catalog.Candidate{Identity: catalog.Identity{ProviderID: provider, InputID: input, ImageID: int64(i + 1)}, Candidate: source.Candidate{ID: fmt.Sprint(i)}})
	}
	return out
}

func TestFairDrawChoosesProvidersAndInputsBeforeCandidateCounts(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(fmt.Sprint(remote), func(t *testing.T) {
			pools := []providerPool{{id: "local", inputs: []inputPool{fairnessPool("local", "large-a", 1000), fairnessPool("local", "large-b", 1000)}}, {id: "wallhaven", inputs: []inputPool{fairnessPool("wallhaven", "small", 10)}}}
			var sizes []int
			chosen, ok := drawCandidate(pools, map[string]bool{}, func(n int) int {
				sizes = append(sizes, n)
				if len(sizes) == 1 && remote {
					return 1
				}
				return 0
			})
			wantProvider := "local"
			wantSizes := []int{2, 2, 1000}
			if remote {
				wantProvider = "wallhaven"
				wantSizes = []int{2, 1, 10}
			}
			if !ok || chosen.Identity.ProviderID != wantProvider || !reflect.DeepEqual(sizes, wantSizes) {
				t.Fatalf("unequal candidate pools biased provider/input draw: provider=%s bounds=%v", chosen.Identity.ProviderID, sizes)
			}
		})
	}
}

func TestFairDrawExhaustsCandidatesAndTriesOtherProviderBeforeRetry(t *testing.T) {
	pools := []providerPool{{id: "local", inputs: []inputPool{fairnessPool("local", "home", 2)}}, {id: "wallhaven", inputs: []inputPool{fairnessPool("wallhaven", "nature", 1)}}}
	blocked := map[string]bool{"local": true}
	chosen, ok := drawCandidate(pools, blocked, func(int) int { return 0 })
	if !ok || chosen.Identity.ProviderID != "wallhaven" {
		t.Fatal("failed provider retried before another available provider")
	}
	for i := 0; i < 2; i++ {
		chosen, ok = drawCandidate(pools, blocked, func(int) int { return 0 })
		if !ok || chosen.Identity.ProviderID != "local" {
			t.Fatal("did not fall back to remaining local candidates")
		}
	}
	if _, ok := drawCandidate(pools, blocked, func(int) int { return 0 }); ok {
		t.Fatal("exhausted candidate selected twice")
	}
}

func TestKnownContentAliasAvoidedWhenAlternativeExists(t *testing.T) {
	service, root := testService(t, &fakeWallpaperBackend{})
	service.request.Source = changedProvider{page: func(p source.Page) source.Page {
		out := []source.Candidate{}
		for _, c := range p.Candidates {
			if filepath.Base(c.Content.Path) == "a.png" {
				out = append(out, c)
			}
		}
		p.Candidates = out
		return p
	}}
	first := act(t, service, "next")
	data, err := os.ReadFile(filepath.Join(root, "a.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.png"), data, 0600); err != nil {
		t.Fatal(err)
	}
	service.request.Source = local.Provider{}
	config, _, err := LoadConfig(service.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	folders, revision := config.localFolders()
	raw, _ := json.Marshal(source.LocalConfig{Root: folders[0].Path, MaxDepth: config.Filters.MaxDepth, Revision: revision})
	if err := discoverMetadata(t.Context(), service.store, service.request.Source, "local", folders[0].ID, revision, folders[0].ID, raw); err != nil {
		t.Fatal(err)
	}
	candidates, err := service.store.Candidates(t.Context(), "local", folders[0].ID, revision, maxCatalogCandidates)
	if err != nil {
		t.Fatal(err)
	}
	file, ok, err := service.store.CachedImage(t.Context(), service.state.History[0].Identity.ImageID)
	if err != nil || !ok {
		t.Fatalf("current cache: %v %v", ok, err)
	}
	found := false
	for _, candidate := range candidates {
		if candidate.Candidate.Content != nil && filepath.Base(candidate.Candidate.Content.Path) == "b.png" {
			found = true
			if err := service.store.SaveCachedImage(t.Context(), candidate.Identity.ImageID, file); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !found {
		t.Fatal("alias fixture not cataloged")
	}
	second := act(t, service, "next")
	if service.currentOriginPath(context.Background()) != filepath.Join(root, "c.png") || second.Current.Path == first.Current.Path {
		t.Fatal("known content alias repeated despite eligible alternative")
	}
}

func TestSingleContentAliasFallbackRemainsAvailable(t *testing.T) {
	service, root := testService(t, &fakeWallpaperBackend{})
	if err := os.Remove(filepath.Join(root, "c.png")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "a.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.png"), data, 0600); err != nil {
		t.Fatal(err)
	}
	first := act(t, service, "next")
	second := act(t, service, "next")
	if second.Current.Path != first.Current.Path || second.HistoryLength != 2 {
		t.Fatalf("single-content fallback unavailable: %+v", second)
	}
}

type offlineReviewProvider struct{}

func (offlineReviewProvider) Describe(context.Context) (source.Description, error) {
	return source.Description{Protocol: source.Protocol, ID: "wallhaven", Name: "Offline fixture", MediaKinds: []string{"image"}, ContentKinds: []string{"https"}, Operations: []string{"describe", "validate", "discover", "resolve"}}, nil
}
func (offlineReviewProvider) Validate(context.Context, json.RawMessage) error { return nil }
func (offlineReviewProvider) Discover(context.Context, source.DiscoverParams) (source.Page, error) {
	return source.Page{}, source.Failure("unavailable", "offline fixture")
}
func (offlineReviewProvider) Resolve(context.Context, source.ResolveParams) (source.Resolved, error) {
	return source.Resolved{}, source.Failure("unavailable", "offline fixture")
}

func TestRemoteOutageUsesCachedImageAndDisabledHistoryIsSkipped(t *testing.T) {
	service, root := testService(t, &fakeWallpaperBackend{})
	first := act(t, service, "next")
	config, _, err := LoadConfig(service.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	config.Providers[0].Enabled = false
	config.Providers = append(config.Providers, ProviderConfig{ID: "wallhaven", Enabled: true, Searches: []SearchConfig{{ID: "nature", Query: "nature"}}})
	writeConfig := func() {
		t.Helper()
		data, err := toml.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(service.request.ConfigPath, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeConfig()
	config, _, err = LoadConfig(service.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	var alternate string
	for _, name := range []string{"a.png", "b.png", "c.png"} {
		path := filepath.Join(root, name)
		if path != service.currentOriginPath(t.Context()) {
			alternate = path
			break
		}
	}
	staged, err := service.cache.Materialize(t.Context(), source.Content{MediaKind: "image", Kind: "local", Path: alternate}, first.Current.Path)
	if err != nil {
		t.Fatal(err)
	}
	candidate := source.Candidate{ID: "remote", MediaKind: "image", Ref: json.RawMessage(`{"id":"remote"}`), Content: &source.Content{MediaKind: "image", Kind: "https", URL: "https://offline.example.com/remote.png"}}
	identity, err := service.store.EnsureImage(t.Context(), "wallhaven", "nature", config.InputRevision("wallhaven", "nature"), "", candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.store.SaveCachedImage(t.Context(), identity.ImageID, catalog.CachedFile{Hash: staged.Hash, Path: staged.Path, Bytes: staged.Bytes, Width: staged.Width, Height: staged.Height}); err != nil {
		t.Fatal(err)
	}
	service.request.Providers = map[string]source.Provider{"wallhaven": offlineReviewProvider{}}
	hint := int64(60)
	service.observeRetry(catalog.RetryKey{ProviderID: "wallhaven", InputID: "nature", Revision: config.InputRevision("wallhaven", "nature"), Scope: "discovery"}, "discovery", &source.Error{Code: "rate_limited", Message: "fixture", RetryAfterSeconds: &hint})
	remote := act(t, service, "next")
	if remote.Current.SourceID != "nature" || remote.Current.Path != staged.Path || len(remote.RefreshWarnings) == 0 {
		t.Fatalf("remote outage lost cached playback: %+v", remote)
	}
	config.Providers[0].Enabled = true
	config.Providers[1].Enabled = false
	writeConfig()
	act(t, service, "next")
	previous := act(t, service, "previous")
	if previous.HistoryCursor != 0 || previous.Current.Path != first.Current.Path || len(previous.Warnings) == 0 {
		t.Fatalf("disabled-provider history not skipped: %+v", previous)
	}
}

func TestDiscoveryAndMaterializationRejectEscapingSymlink(t *testing.T) {
	service, root := testService(t, &fakeWallpaperBackend{})
	outside := filepath.Join(t.TempDir(), "outside.png")
	writeScanImage(t, outside, 2, 2)
	link := filepath.Join(root, "escape.png")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	config, _, err := LoadConfig(service.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	folders, revision := config.localFolders()
	if got := localDiscoveryDimensions(t.Context(), source.LocalConfig{Root: root, MaxDepth: 2}, link); got != nil {
		t.Fatal("discovery inspected an escaping symlink")
	}
	ref, err := json.Marshal(map[string]string{"path": link})
	if err != nil {
		t.Fatal(err)
	}
	candidate := source.Candidate{ID: "escape.png", MediaKind: "image", Ref: ref, Content: &source.Content{MediaKind: "image", Kind: "local", Path: link}}
	id, err := service.store.EnsureImage(t.Context(), "local", folders[0].ID, revision, folders[0].ID, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.materializeChoice(t.Context(), playbackChoice{identity: id, historyIndex: -1}, config); err == nil {
		t.Fatal("playback materialized an escaping symlink")
	}
}

func TestLocalSelectionUsesDiscoveredHeadersWithoutOpeningOriginals(t *testing.T) {
	service, root := testService(t, &fakeWallpaperBackend{})
	refreshForTest(t, service)
	config, _, err := LoadConfig(service.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(root, "a.png")
	if err := os.Remove(original); err != nil {
		t.Fatal(err)
	}
	countCandidates := func() int {
		t.Helper()
		pools, err := service.candidatePools(t.Context(), config, maxCatalogCandidates, func(string) {})
		if err != nil || len(pools) != 1 || len(pools[0].inputs) != 1 {
			t.Fatalf("unexpected pools: %v %v", pools, err)
		}
		return len(pools[0].inputs[0])
	}
	if got := countCandidates(); got != 3 {
		t.Fatalf("selection opened originals before drawing: %d candidates", got)
	}
	refreshForTest(t, service)
	if got := countCandidates(); got != 2 {
		t.Fatalf("completed refresh still drew the removed original: %d candidates", got)
	}
}

func TestCandidateBudgetIsSharedAcrossInputsAndRejectsOverflow(t *testing.T) {
	service, _ := testService(t, &fakeWallpaperBackend{})
	secondRoot := t.TempDir()
	writeScanImage(t, filepath.Join(secondRoot, "other.png"), 2, 2)
	before := settings(t, service)
	if _, err := service.AddFolder(t.Context(), AddFolderRequest{Revision: before.Revision, Path: secondRoot}); err != nil {
		t.Fatal(err)
	}
	refreshForTest(t, service)
	config, _, err := LoadConfig(service.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	ignore := func(string) {}
	pools, err := service.candidatePools(t.Context(), config, 4, ignore)
	if err != nil || len(pools) != 1 || len(pools[0].inputs) != 2 {
		t.Fatalf("exact shared budget rejected: pools=%v error=%v", pools, err)
	}
	if pools, err := service.candidatePools(t.Context(), config, 3, ignore); !errors.Is(err, catalog.ErrCandidateLimit) || pools != nil {
		t.Fatalf("overflow should reject both inputs instead of truncating: pools=%v error=%v", pools, err)
	}
}
