package playback

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/source"
	"github.com/vadim82/panora/internal/sources/local"
)

type changedProvider struct {
	local.Provider
	page    func(source.Page) source.Page
	resolve func(source.Resolved) source.Resolved
	listing func(context.Context, source.DiscoverParams) (source.Page, error)
}

func (p changedProvider) Discover(ctx context.Context, params source.DiscoverParams) (source.Page, error) {
	if p.listing != nil {
		return p.listing(ctx, params)
	}
	page, err := p.Provider.Discover(ctx, params)
	if p.page != nil {
		page = p.page(page)
	}
	return page, err
}

func (p changedProvider) Resolve(ctx context.Context, params source.ResolveParams) (source.Resolved, error) {
	result, err := p.Provider.Resolve(ctx, params)
	if p.resolve != nil {
		result = p.resolve(result)
	}
	return result, err
}

type failingProvider struct{ local.Provider }

func (failingProvider) Discover(context.Context, source.DiscoverParams) (source.Page, error) {
	return source.Page{}, errors.New("discovery failed")
}

func TestDiscoveryOutageUsesCommittedCandidates(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	service, _ := testService(t, backend)
	first := act(t, service, "next")
	service.request.Source = failingProvider{}
	second := act(t, service, "next")
	if second.HistoryLength != 2 || second.Uncertain || second.Current.Path == first.Current.Path || len(second.RefreshWarnings) == 0 || backend.calls != 2 {
		t.Fatalf("committed fallback unavailable: %+v", second)
	}
}

func metadataCandidate(id string) source.Candidate {
	return source.Candidate{ID: id, MediaKind: "image", Ref: json.RawMessage(`{"id":"reference"}`), Content: &source.Content{MediaKind: "image", Kind: "https", URL: "https://example.org/" + id + ".png"}, Dimensions: &source.Dimensions{Width: 5120, Height: 1440}, Attribution: &source.Attribution{PageURL: "https://example.org/view/" + id, Author: "Artist"}}
}

func TestMetadataDiscoveryResumesCommittedCursorAfterRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := catalog.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	provider := changedProvider{listing: func(_ context.Context, p source.DiscoverParams) (source.Page, error) {
		calls++
		if calls == 1 {
			if !source.IsNull(p.Cursor) {
				t.Fatal("fresh scan had a cursor")
			}
			return source.Page{Candidates: []source.Candidate{metadataCandidate("first")}, NextCursor: json.RawMessage(`{"page":2}`)}, nil
		}
		return source.Page{}, errors.New("network interrupted")
	}}
	config := json.RawMessage(`{"root":"/","max_depth":0}`)
	if err := discoverMetadata(ctx, store, metadataProvider{provider}, "wallhaven", "mountains", "rev", "", config); err == nil {
		t.Fatal("accepted partial discovery")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = catalog.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	provider.listing = func(_ context.Context, p source.DiscoverParams) (source.Page, error) {
		if string(p.Cursor) != `{"page":2}` {
			t.Fatalf("lost committed cursor: %s", p.Cursor)
		}
		return source.Page{Candidates: []source.Candidate{metadataCandidate("second")}, NextCursor: json.RawMessage("null")}, nil
	}
	if err := discoverMetadata(ctx, store, metadataProvider{provider}, "wallhaven", "mountains", "rev", "", config); err != nil {
		t.Fatal(err)
	}
	candidates, err := store.Candidates(ctx, "wallhaven", "mountains", "rev", maxCatalogCandidates)
	if err != nil || len(candidates) != 2 {
		t.Fatalf("lost committed candidates: %+v %v", candidates, err)
	}
}

type metadataProvider struct{ changedProvider }

func (metadataProvider) Validate(context.Context, json.RawMessage) error { return nil }
func (metadataProvider) Describe(ctx context.Context) (source.Description, error) {
	d, err := (local.Provider{}).Describe(ctx)
	d.ID = "wallhaven"
	d.ContentKinds = []string{"https"}
	return d, err
}

func TestServiceCatalogsEnabledRemoteMetadataWithoutRemotePlayback(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	service, _ := testService(t, backend)
	calls := 0
	remote := changedProvider{listing: func(context.Context, source.DiscoverParams) (source.Page, error) {
		calls++
		return source.Page{Candidates: []source.Candidate{metadataCandidate("mountain")}, NextCursor: json.RawMessage("null")}, nil
	}}
	service.request.Providers = map[string]source.Provider{"wallhaven": metadataProvider{remote}}
	data, err := os.ReadFile(service.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("\n[[providers]]\nid='wallhaven'\nenabled=true\n[[providers.searches]]\nid='mountains'\nquery='mountain'\n")...)
	if err := os.WriteFile(service.request.ConfigPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	status := act(t, service, "next")
	config, _, err := LoadConfig(service.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	revision := config.InputRevision("wallhaven", "mountains")
	candidates, err := service.store.Candidates(context.Background(), "wallhaven", "mountains", revision, maxCatalogCandidates)
	if err != nil || len(candidates) != 1 || calls != 1 {
		t.Fatalf("remote metadata not stored: %+v calls=%d err=%v", candidates, calls, err)
	}
	if status.Current.SourceID != "home" || backend.calls != 1 {
		t.Fatalf("remote discovery altered local playback: %+v", status)
	}
	act(t, service, "next")
	before := calls
	act(t, service, "previous")
	act(t, service, "next")
	if calls != before {
		t.Fatalf("history navigation performed remote discovery: %d -> %d", before, calls)
	}
}

type wrongProtocolProvider struct{ local.Provider }

func (wrongProtocolProvider) Describe(ctx context.Context) (source.Description, error) {
	d, err := (local.Provider{}).Describe(ctx)
	d.Protocol = 1
	return d, err
}

func TestUntrustedProviderFailuresNeverReachDesktop(t *testing.T) {
	for _, mode := range []string{"protocol", "outside root", "remote local page", "missing cursor", "resolve substitution", "truncated image"} {
		t.Run(mode, func(t *testing.T) {
			backend := &fakeWallpaperBackend{}
			service, root := testService(t, backend)
			outside := filepath.Join(t.TempDir(), "outside.png")
			writeScanImage(t, outside, 2, 2)
			switch mode {
			case "protocol":
				service.request.Source = wrongProtocolProvider{}
			case "outside root":
				service.request.Source = changedProvider{page: func(p source.Page) source.Page {
					for i := range p.Candidates {
						p.Candidates[i].Content.Path = outside
					}
					return p
				}}
			case "remote local page":
				service.request.Source = changedProvider{page: func(p source.Page) source.Page {
					for i := range p.Candidates {
						p.Candidates[i].Content = &source.Content{MediaKind: "image", Kind: "https", URL: "https://example.org/image.png"}
					}
					return p
				}}
			case "missing cursor":
				service.request.Source = changedProvider{page: func(p source.Page) source.Page { p.NextCursor = nil; return p }}
			case "resolve substitution":
				service.request.Source = changedProvider{resolve: func(r source.Resolved) source.Resolved { r.Content.Path = outside; return r }}
			case "truncated image":
				for _, name := range []string{"a.png", "b.png", "c.png"} {
					path := filepath.Join(root, name)
					data, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, data[:len(data)-16], 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			refreshForTest(t, service)
			status, err := service.Action(context.Background(), "next")
			if err == nil || backend.calls != 0 || status.HistoryLength != 0 || status.Uncertain {
				t.Fatalf("untrusted provider reached desktop: %+v %v calls=%d", status, err, backend.calls)
			}
		})
	}
}
