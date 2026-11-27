package playback

import (
	"context"
	"time"

	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/source"
)

// SearchCandidates reports a saved search's current selection
// pool. Uncached candidates can still fail original validation during playback.
type SearchCandidates struct {
	Version        int            `json:"version"`
	Discovered     int            `json:"discovered"`
	Eligible       *int           `json:"eligible"`
	RuleResolution RuleResolution `json:"rule_resolution"`
	SearchURL      string         `json:"search_url,omitempty"`
}

type wallhavenBrowserSearch interface {
	source.Provider
	BrowserSearchURL(source.WallhavenConfig, *source.FilterHints) string
}

func (s *Service) CountSearchCandidates(ctx context.Context, id, revision string) (SearchCandidates, error) {
	return s.countSavedCandidates(ctx, "wallhaven", id, revision)
}

func (s *Service) countSavedCandidates(ctx context.Context, providerID, id, revision string) (SearchCandidates, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()

	if !s.configMu.TryRLock() {
		return SearchCandidates{}, &Error{Code: "busy", Message: "settings are being saved; count candidates after the save finishes"}
	}
	config, _, saved, err := ReadConfig(s.request.ConfigPath)
	s.statusMu.RLock()
	backend, exclusions := s.status.Backend, s.status.previewExclusions
	s.statusMu.RUnlock()
	s.configMu.RUnlock()
	if err != nil {
		return SearchCandidates{}, invalidSavedConfiguration()
	}
	if configRevision(saved) != revision {
		return SearchCandidates{}, &Error{Code: "config_conflict", Message: "settings changed; reload them before counting candidates"}
	}
	var wallhavenSearch *SearchConfig
	if providerID == "wallhaven" {
		providerConfig, index := findSearch(&config, id)
		if index < 0 || !providerConfig.Enabled || !providerConfig.Searches[index].enabled() {
			return SearchCandidates{}, &Error{Code: "search_not_found", Message: "the enabled Wallhaven search was not found"}
		}
		wallhavenSearch = &providerConfig.Searches[index]
	} else if _, ok := configuredInputRules(config, providerID, id); !ok || !s.request.inputProviderAvailable(providerID) {
		return SearchCandidates{}, &Error{Code: "search_not_found", Message: "the enabled Alpha Coders source was not found"}
	}
	identity := catalog.Identity{ProviderID: providerID, InputID: id, Revision: config.InputRevision(providerID, id)}
	rules, ratio, resolution := s.resolveSourceRulesForBackend(ctx, config, identity, backend)
	rows, err := s.store.Candidates(ctx, providerID, id, identity.Revision, maxCatalogCandidates)
	if err != nil {
		return SearchCandidates{}, &Error{Code: "candidate_count_failed", Message: "could not count discovered candidates"}
	}
	result := SearchCandidates{Version: StatusVersion, Discovered: len(rows), RuleResolution: resolution}
	if provider, ok := s.request.provider(providerID).(wallhavenBrowserSearch); ok && wallhavenSearch != nil {
		if description, err := provider.Describe(ctx); err == nil {
			result.SearchURL = provider.BrowserSearchURL(wallhavenSearch.settings(identity.Revision), discoveryFilter(rules, ratio, description.FilterHints))
		}
	}
	if resolution.State != "resolved" {
		return result, nil
	}
	eligible := 0
	for _, candidate := range rows {
		if ctx.Err() != nil {
			return SearchCandidates{}, &Error{Code: "candidate_count_failed", Message: "counting discovered candidates timed out"}
		}
		if !exclusions.Contains(candidate.Identity.ImageID, candidate.Hash) && matchesKnownDimensions(candidate, rules, ratio) {
			eligible++
		}
	}
	result.Eligible = &eligible
	return result, nil
}
