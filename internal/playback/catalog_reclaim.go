package playback

import (
	"context"
	"time"

	"github.com/vadim82/panora/internal/catalog"
)

const retiredCatalogGrace = 24 * time.Hour
const reclaimPlaybackLead = 100 * time.Millisecond

// Reclaim only between actions so an uncommitted selection cannot lose its
// membership. Never queue playback behind a long scan or a configuration writer.
func (s *Service) reclaimCatalog(cursor catalog.ReclaimCursor) catalog.ReclaimCursor {
	s.statusMu.RLock()
	setupRequired, scheduled := s.status.SetupRequired, s.status.NextAt
	s.statusMu.RUnlock()
	if !setupRequired && scheduled != nil && time.Until(*scheduled) <= reclaimPlaybackLead {
		return cursor
	}
	if !s.configMu.TryRLock() {
		return cursor
	}
	defer s.configMu.RUnlock()
	config, _, err := LoadConfig(s.request.ConfigPath)
	if err != nil {
		return cursor
	}
	var inputs []catalog.InputRevision
	for _, p := range config.Providers {
		for _, input := range providerInputs(p, config.Filters.MaxDepth, sourceDefaultRules(config)) {
			inputs = append(inputs, catalog.InputRevision{ProviderID: p.ID, InputID: input.id, Revision: input.revision})
		}
	}
	if theme := config.OmarchyTheme; theme != nil {
		input := catalog.InputRevision{ProviderID: omarchyProvider, InputID: theme.ID}
		if observed := s.theme.observation.Load(); observed != nil {
			input.Revision = observed.active.ID
		}
		inputs = append(inputs, input)
		if collection := s.theme.collection.Load(); collection != nil && collection.sourceID == theme.ID && collection.active.ID != input.Revision {
			inputs = append(inputs, catalog.InputRevision{ProviderID: omarchyProvider, InputID: theme.ID, Revision: collection.active.ID})
		}
	}
	if !s.actionMu.TryLock() {
		return cursor
	}
	defer s.actionMu.Unlock()
	if !s.state.Paused && !setupRequired && !s.nextAt.IsZero() && time.Until(s.nextAt) <= reclaimPlaybackLead {
		return cursor
	}
	if !s.retryMu.TryLock() {
		return cursor
	}
	defer s.retryMu.Unlock()
	ctx, cancel := context.WithTimeout(s.ctx, 75*time.Millisecond)
	defer cancel()
	next, retired, err := s.store.ReclaimObsolete(ctx, inputs, time.Now().Add(-retiredCatalogGrace), cursor)
	if err != nil {
		return cursor
	}
	for _, key := range retired {
		delete(s.retries, key)
	}
	return next
}
