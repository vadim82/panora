package playback

import (
	"context"
	"net/url"
	"testing"

	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/source"
	"github.com/vadim82/panora/internal/sources/wallhaven"
)

func TestSearchCandidateCountsUseEffectiveRulesAndExclusions(t *testing.T) {
	s, _, backend := configurationFixture(t, `version=1
[filters]
min_width=1920
min_height=1080
ratio="16:9"

[[providers]]
id="wallhaven"
enabled=true

[[providers.searches]]
id="shared"
query="stars"

[[providers.searches]]
id="custom"
query=""
tags=["space", "space art"]
tag_match="any"
colors=["#7d82d9", "#060b1e", "#a3bfd1"]
[providers.searches.image_rules]
min_width=5120
min_height=1440
ratio="monitor-similar"
`)
	s.request.Providers = map[string]source.Provider{"wallhaven": wallhaven.New()}
	s.request.InspectCapabilities = func(context.Context, string) (desktop.Capabilities, error) {
		return knownMonitorCapabilities("macos", 5120, 1440), nil
	}
	config, _, data, err := ReadConfig(s.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	revision := configRevision(data)
	for _, id := range []string{"shared", "custom"} {
		for _, fixture := range []struct {
			name          string
			width, height int
		}{
			{"wide", 5120, 1440},
			{"standard", 1920, 1080},
			{"too-small", 3840, 1080},
			{"excluded-wide", 7680, 2160},
			{"wrong-shape", 5120, 2160},
		} {
			item := metadataCandidate(fixture.name)
			item.Dimensions = &source.Dimensions{Width: fixture.width, Height: fixture.height}
			identity, err := s.store.EnsureImage(t.Context(), "wallhaven", id, config.InputRevision("wallhaven", id), "", item)
			if err != nil {
				t.Fatal(err)
			}
			if fixture.name == "excluded-wide" {
				s.exclusions, err = s.store.ExcludeImage(t.Context(), identity.ImageID)
				if err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	s.publishStatus()
	for _, id := range []string{"shared", "custom"} {
		count, err := s.CountSearchCandidates(t.Context(), id, revision)
		if err != nil || count.Discovered != 5 || count.Eligible == nil || *count.Eligible != 1 {
			t.Fatalf("%s count: %+v %v", id, count, err)
		}
		address, err := url.Parse(count.SearchURL)
		if err != nil || address.Scheme != "https" || address.Host != "wallhaven.cc" || address.Path != "/search" {
			t.Fatalf("search URL: %s %v", count.SearchURL, err)
		}
		q := address.Query()
		if id == "shared" && (q.Get("atleast") != "1920x1080" || q.Get("ratios") != "16x9") {
			t.Fatalf("shared filters lost: %v", q)
		}
		if id == "custom" && (q.Get("atleast") != "5120x1440" || q.Has("ratios") ||
			q.Get("q") != `+space | +"space art"` || q.Get("colors") != "999999,000000,cccccc" ||
			q.Get("purity") != "100") {
			t.Fatalf("custom filters: %v", q)
		}
	}
	s.request.InspectCapabilities = func(context.Context, string) (desktop.Capabilities, error) {
		return desktop.Capabilities{}, nil
	}
	unresolved, err := s.CountSearchCandidates(t.Context(), "custom", revision)
	if err != nil || unresolved.Discovered != 5 || unresolved.Eligible != nil || unresolved.RuleResolution.State != "unavailable" {
		t.Fatalf("unresolved geometry count: %+v %v", unresolved, err)
	}
	_, err = s.CountSearchCandidates(t.Context(), "custom", "stale")
	requireConfigError(t, err, "config_conflict")
	_, err = s.CountSearchCandidates(t.Context(), "removed", revision)
	requireConfigError(t, err, "search_not_found")
	if backend.calls.Load() != 0 {
		t.Fatal("candidate counts changed playback")
	}
}
