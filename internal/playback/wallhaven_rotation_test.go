package playback

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/source"
)

type monitorDiscoveryProvider struct {
	offlineReviewProvider
	discover func(source.DiscoverParams) (source.Page, error)
}

func (p monitorDiscoveryProvider) Describe(ctx context.Context) (source.Description, error) {
	description, err := p.offlineReviewProvider.Describe(ctx)
	description.FilterHints = []string{"min_width", "min_height", "ratio"}
	return description, err
}

func (p monitorDiscoveryProvider) Discover(_ context.Context, params source.DiscoverParams) (source.Page, error) {
	return p.discover(params)
}

func TestMonitorDiscoveryRestartsPaginationWhenRatioChanges(t *testing.T) {
	s, _, _ := configurationFixture(t, `version=1
[filters]
min_width=5120
min_height=1440
ratio="monitor"
[[providers]]
id="wallhaven"
enabled=true
[[providers.searches]]
id="nature"
query="nature"
`)
	width, height := 5120, 1440
	calls := 0
	s.request.InspectCapabilities = func(ctx context.Context, backend string) (desktop.Capabilities, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("discovery geometry query is unbounded")
		}
		calls++
		return knownMonitorCapabilities(backend, width, height), nil
	}
	type request struct {
		ratio  source.Dimensions
		cursor string
	}
	var requests []request
	s.request.Providers = map[string]source.Provider{"wallhaven": monitorDiscoveryProvider{
		discover: func(params source.DiscoverParams) (source.Page, error) {
			if params.Filter == nil || params.Filter.MinWidth != 5120 || params.Filter.MinHeight != 1440 {
				t.Fatalf("minimum hints missing: %+v", params.Filter)
			}
			var ratio source.Dimensions
			if params.Filter.Ratio != nil {
				ratio = *params.Filter.Ratio
			}
			requests = append(requests, request{ratio, string(params.Cursor)})
			if !source.IsNull(params.Cursor) {
				return source.Page{}, source.Failure("invalid_cursor", "old fixture geometry")
			}
			next := json.RawMessage("null")
			if ratio.Width == 32 {
				next = json.RawMessage(`{"page":2}`)
			}
			return source.Page{Candidates: []source.Candidate{}, NextCursor: next}, nil
		},
	}}
	config, _, err := LoadConfig(s.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	p := config.Providers[0]
	input := providerInputs(p, config.Filters.MaxDepth, sourceDefaultRules(config))[0]
	if counts, err := s.refreshInputCounts(t.Context(), p, input, 1); err == nil || counts.Pages != 1 {
		t.Fatalf("first refresh did not leave a saved cursor: %+v %v", counts, err)
	}
	width, height = 3840, 2160
	if _, err := s.refreshInputCounts(t.Context(), p, input, 2); err != nil {
		t.Fatal(err)
	}
	width, height = 0, 0
	if _, err := s.refreshInputCounts(t.Context(), p, input, 1); err != nil {
		t.Fatal(err)
	}
	want := []request{
		{source.Dimensions{Width: 32, Height: 9}, "null"},
		{source.Dimensions{Width: 16, Height: 9}, `{"page":2}`},
		{source.Dimensions{Width: 16, Height: 9}, "null"},
		{source.Dimensions{}, "null"},
	}
	if !reflect.DeepEqual(requests, want) || calls != 3 {
		t.Fatalf("geometry changed between pages or scan did not restart: requests=%+v geometry reads=%d", requests, calls)
	}
}

func knownMonitorCapabilities(backend string, width, height int) desktop.Capabilities {
	caps := testDesktopCapabilities(backend)
	caps.Geometry = desktop.Geometry{State: "known", Displays: []desktop.DisplayGeometry{{Name: "test", Width: width, Height: height, Scale: 1}}}
	return caps
}

type resolvingWallhaven struct {
	offlineReviewProvider
	resolvedIDs []string
}

func (p *resolvingWallhaven) Resolve(_ context.Context, params source.ResolveParams) (source.Resolved, error) {
	p.resolvedIDs = append(p.resolvedIDs, params.ID)
	return source.Resolved{}, source.Failure("unavailable", "fixture resolution failed")
}

func TestSparseWallhavenPoolPreservesMixedRotation(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		mixed    bool
		shuffle  bool
		remote   string
		want     string
		resolves []string
	}{
		{name: "mixed chooses eligible remote", mixed: true, remote: "cached", want: "nature"},
		{name: "remote only skips known mismatches", remote: "cached", want: "nature"},
		{name: "mixed with no eligible remote falls back", mixed: true, want: "home"},
		{name: "mixed with failing remote falls back", mixed: true, remote: "failed", want: "home", resolves: []string{"failed"}},
		{name: "unknown dimensions still get resolved", mixed: true, remote: "unknown", want: "home", resolves: []string{"unknown"}},
		{name: "misleading matching dimensions still validate bytes", remote: "misleading"},
		{name: "shuffle tries another image after decoded bytes fail", mixed: true, shuffle: true, remote: "misleading", want: "home"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			body := "version=1\n"
			if scenario.mixed {
				root := t.TempDir()
				writeScanImage(t, filepath.Join(root, "Local.png"), 16, 16)
				body = localTOML(root)
			}
			body += `
[[providers]]
id="wallhaven"
enabled=true
[[providers.searches]]
id="nature"
query="nature"
[providers.searches.image_rules]
min_width=64
min_height=18
ratio="monitor"
`
			if scenario.shuffle {
				body += "\n[playback]\nrotation_mode='shuffle_all_images'\n"
			}
			s, _, _ := configurationFixture(t, body)
			s.request.InspectCapabilities = func(_ context.Context, backend string) (desktop.Capabilities, error) {
				return knownMonitorCapabilities(backend, 5120, 1440), nil
			}
			remote := &resolvingWallhaven{}
			s.request.Providers = map[string]source.Provider{"wallhaven": remote}
			config, _, err := LoadConfig(s.request.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			if scenario.mixed {
				p := config.Providers[0]
				input := providerInputs(p, config.Filters.MaxDepth, sourceDefaultRules(config))[0]
				if _, err := s.refreshInputCounts(t.Context(), p, input, 10); err != nil {
					t.Fatal(err)
				}
			}

			p := config.Providers[len(config.Providers)-1]
			add := func(id string, dims *source.Dimensions) catalog.Identity {
				t.Helper()
				candidate := metadataCandidate(id)
				candidate.Dimensions = dims
				identity, err := s.store.EnsureImage(t.Context(), p.ID, "nature", config.InputRevision(p.ID, "nature"), "", candidate)
				if err != nil {
					t.Fatal(err)
				}
				return identity
			}
			stage := func(id catalog.Identity, width, height, recordedHeight int) {
				t.Helper()
				path := filepath.Join(t.TempDir(), "Fixture.png")
				writeScanImage(t, path, width, height)
				original, err := s.cache.Materialize(t.Context(), source.Content{Kind: "local", MediaKind: "image", Path: path})
				if err != nil {
					t.Fatal(err)
				}
				err = s.store.SaveCachedImage(t.Context(), id.ImageID, catalog.CachedFile{
					Path: original.Path, Hash: original.Hash, Width: width, Height: recordedHeight, Bytes: original.Bytes,
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			bad := &source.Dimensions{Width: 64, Height: 64}
			good := &source.Dimensions{Width: 64, Height: 18}
			small := &source.Dimensions{Width: 32, Height: 9}
			for i := range 45 {
				dims := bad
				if i%2 != 0 {
					dims = small
				}
				add(fmt.Sprintf("mismatch-%d", i), dims)
			}
			// A new listing can report dimensions that disagree with the bytes
			// already known to playback. Use those decoded dimensions first.
			stage(add("known-decoded-mismatch", good), 64, 64, 64)

			switch scenario.remote {
			case "cached":
				stage(add("known-decoded-match", bad), 64, 18, 18)
			case "misleading":
				// Deliberately poison the recorded dimensions too so the
				// candidate reaches reuse and checks its actual bytes.
				stage(add("misleading", good), 64, 65, 18)
			case "unknown":
				add("unknown", nil)
			case "failed":
				add("failed", good)
			}
			draws := 0
			s.draw = func(n int) int {
				draws++
				if draws == 1 && n == 2 {
					return 1 // Give Wallhaven the first choice in the mixed pool.
				}
				return 0
			}
			status, err := s.Action(t.Context(), "next")
			if scenario.want == "" {
				requireConfigError(t, err, "selection_failed")
				if len(status.Warnings) != 1 || !strings.Contains(status.Warnings[0], "decoded image") || status.HistoryLength != 0 {
					t.Fatalf("nonmatching bytes were accepted or did not reach validation: %+v", status)
				}
			} else if err != nil || status.Current == nil || status.Current.SourceID != scenario.want {
				t.Fatalf("want source %q, status=%+v err=%v", scenario.want, status, err)
			}
			if scenario.shuffle && (len(status.Warnings) != 1 || !strings.Contains(status.Warnings[0], "decoded image")) {
				t.Fatalf("shuffle did not validate decoded bytes before fallback: %+v", status)
			}
			if !reflect.DeepEqual(remote.resolvedIDs, scenario.resolves) {
				t.Fatalf("unexpected remote requests; resolved=%v want=%v", remote.resolvedIDs, scenario.resolves)
			}
		})
	}
}

func TestSimilarMonitorDiscoveryKeepsBoundedCursorAcrossGeometryChanges(t *testing.T) {
	s, _, _ := configurationFixture(t, `version=1
[[providers]]
id="wallhaven"
enabled=true
[[providers.searches]]
id="nature"
query="nature"
[providers.searches.image_rules]
min_width=3024
min_height=1964
ratio="monitor-similar"
`)
	width, height := 3024, 1964
	geometryQueries := 0
	s.request.InspectCapabilities = func(_ context.Context, backend string) (desktop.Capabilities, error) {
		geometryQueries++
		return knownMonitorCapabilities(backend, width, height), nil
	}
	var cursors []string
	s.request.Providers = map[string]source.Provider{"wallhaven": monitorDiscoveryProvider{
		discover: func(params source.DiscoverParams) (source.Page, error) {
			filter := params.Filter
			if filter == nil || filter.MinWidth != 3024 || filter.MinHeight != 1964 || filter.Ratio != nil || len(filter.Ratios) != 0 {
				t.Fatalf("similar discovery hints: %+v", filter)
			}
			cursors = append(cursors, string(params.Cursor))
			if source.IsNull(params.Cursor) {
				return source.Page{Candidates: []source.Candidate{}, NextCursor: json.RawMessage(`{"page":2}`)}, nil
			}
			return source.Page{Candidates: []source.Candidate{}, NextCursor: json.RawMessage("null")}, nil
		},
	}}
	config, _, err := LoadConfig(s.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	p := config.Providers[0]
	input := providerInputs(p, config.Filters.MaxDepth, sourceDefaultRules(config))[0]
	if counts, err := s.refreshInputCounts(t.Context(), p, input, 1); err == nil || counts.Pages != 1 {
		t.Fatalf("first refresh did not keep a checkpoint at the page budget: %+v %v", counts, err)
	}
	image := ImageInfo{Width: 3600, Height: 2400}
	identity := catalog.Identity{ProviderID: "wallhaven", InputID: "nature"}
	rules, ratio, resolution := s.resolveSourceRules(t.Context(), config, identity)
	if resolution.State != "resolved" || !rules.matches(image, ratio) {
		t.Fatalf("first geometry should admit a 3:2 candidate: %+v", resolution)
	}
	width, height = 3840, 2160
	if counts, err := s.refreshInputCounts(t.Context(), p, input, 1); err != nil || counts.Pages != 1 {
		t.Fatalf("second refresh did not complete from cursor: %+v %v", counts, err)
	}
	rules, ratio, resolution = s.resolveSourceRules(t.Context(), config, identity)
	if resolution.State != "resolved" || rules.matches(image, ratio) {
		t.Fatalf("new geometry should reject a 3:2 candidate: %+v", resolution)
	}
	if !reflect.DeepEqual(cursors, []string{"null", `{"page":2}`}) || geometryQueries != 2 {
		t.Fatalf("discovery should reuse the shape-independent cursor without geometry reads: %v, queries=%d", cursors, geometryQueries)
	}
}

func TestSimilarMonitorSearchSaveAndPlayback(t *testing.T) {
	s, _, backend := configurationFixture(t, `version=1
[[providers]]
id="wallhaven"
enabled=true
[[providers.searches]]
id="nature"
query="nature"
[providers.searches.image_rules]
min_width=756
min_height=491
ratio="monitor"
`)
	caps := knownMonitorCapabilities("macos", 756, 491)
	s.request.InspectCapabilities = func(context.Context, string) (desktop.Capabilities, error) {
		return caps, nil
	}
	remote := &resolvingWallhaven{}
	s.request.Providers = map[string]source.Provider{"wallhaven": remote}
	before := settings(t, s)
	rules := *before.Searches[0].ImageRules
	rules.Ratio = "monitor-similar"
	saved, err := s.UpdateSearch(t.Context(), UpdateSearchRequest{Revision: before.Revision, ID: "nature", ImageRules: &rules})
	if err != nil || saved.Revision == before.Revision {
		t.Fatalf("save similar shape: %v", err)
	}
	_, err = s.UpdateSearch(t.Context(), UpdateSearchRequest{Revision: before.Revision, ID: "nature", ImageRules: before.Searches[0].ImageRules})
	requireConfigError(t, err, "config_conflict")
	reopened := settings(t, s)
	if !reflect.DeepEqual(reopened.Searches[0].ImageRules, &rules) || reopened.Revision != saved.Revision {
		t.Fatalf("reopened rules differ: %+v", reopened.Searches[0].ImageRules)
	}
	config, _, err := LoadConfig(s.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	add := func(id string, listedWidth, listedHeight, actualHeight int) {
		t.Helper()
		candidate := metadataCandidate(id)
		candidate.Dimensions = &source.Dimensions{Width: listedWidth, Height: listedHeight}
		identity, err := s.store.EnsureImage(t.Context(), "wallhaven", "nature", config.InputRevision("wallhaven", "nature"), "", candidate)
		if err != nil {
			t.Fatal(err)
		}
		if actualHeight == 0 {
			return
		}
		path := filepath.Join(t.TempDir(), "Fixture.png")
		writeScanImage(t, path, listedWidth, actualHeight)
		original, err := s.cache.Materialize(t.Context(), source.Content{Kind: "local", MediaKind: "image", Path: path})
		if err != nil {
			t.Fatal(err)
		}
		err = s.store.SaveCachedImage(t.Context(), identity.ImageID, catalog.CachedFile{
			Path: original.Path, Hash: original.Hash, Width: listedWidth, Height: listedHeight, Bytes: original.Bytes,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	add("metadata-mismatch", 960, 540, 0)
	add("decoded-mismatch", 960, 600, 540)
	s.draw = func(n int) int { return 0 }
	status, err := s.Action(t.Context(), "next")
	requireConfigError(t, err, "selection_failed")
	if status.HistoryLength != 0 || len(status.Warnings) != 1 || !strings.Contains(status.Warnings[0], "decoded image") || len(remote.resolvedIDs) != 0 {
		t.Fatalf("mismatches were not rejected by metadata and decoded validation: %+v, resolved=%v", status, remote.resolvedIDs)
	}
	add("nearby-shape", 960, 600, 600)
	status = act(t, s, "next")
	if status.Current == nil || status.HistoryLength != 1 || backend.calls.Load() != 1 {
		t.Fatalf("nearby shape was not accepted: %+v", status)
	}
	for _, state := range []string{"ambiguous", "unavailable"} {
		caps.Geometry = desktop.Geometry{State: state}
		preview, err := s.PreviewRules(t.Context(), RulePreviewRequest{
			Revision: reopened.Revision, ProviderID: "wallhaven", InputID: "nature",
			Rules: ImageRules{MinWidth: 756, MinHeight: 491, Ratio: "monitor-similar"},
		})
		if err != nil || preview.RuleResolution.State != state {
			t.Fatalf("preview geometry %s: %+v %v", state, preview, err)
		}
		_, err = s.Action(t.Context(), "next")
		requireConfigError(t, err, "rule_unavailable")
	}
	if s.Status().Current.ID != status.Current.ID || s.Status().HistoryLength != 1 || backend.calls.Load() != 1 {
		t.Fatal("geometry failure changed accepted wallpaper or history")
	}
}
