package playback

import (
	"context"
	"errors"
	"time"

	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/source"
)

const MaxHistoryPageSize = 10

// Ten entries with eight maximally escaped 4096-byte origin/attribution fields
// plus IDs and envelope fit within 2 MiB.
const MaxHistoryMessageBytes = 2 << 20

type HistoryEntry struct {
	ID           string              `json:"id"`
	ImageID      int64               `json:"image_id"`
	Provider     string              `json:"provider"`
	SourceID     string              `json:"source_id"`
	Image        string              `json:"image"`
	OriginalPath string              `json:"original_path,omitempty"`
	AcceptedAt   time.Time           `json:"accepted_at"`
	Current      bool                `json:"current"`
	Dimensions   *source.Dimensions  `json:"dimensions"`
	Attribution  *source.Attribution `json:"attribution,omitempty"`
	Availability string              `json:"availability"`
	Reason       string              `json:"reason"`
}

type HistoryPage struct {
	Version    int            `json:"version"`
	Entries    []HistoryEntry `json:"entries"`
	CurrentID  string         `json:"current_id,omitempty"`
	NextBefore string         `json:"next_before,omitempty"`
}

// ValidHistoryReference checks the opaque handle used by the history API.
func ValidHistoryReference(id string) bool {
	return catalog.ValidHistoryReference(id)
}

// History lists at most ten retained entries without downloads, image decoding,
// provider calls. Match monitor performs bounded read-only geometry inspection.
// Cache presence is not a promise of replay:
// replay always revalidates bytes against current rules.
func (s *Service) History(ctx context.Context, before string, limit int) (HistoryPage, error) {
	if limit < 1 || limit > MaxHistoryPageSize || before != "" && !ValidHistoryReference(before) {
		return HistoryPage{}, &Error{Code: "invalid_request", Message: "history limit must be 1 through 10 and before must be a retained history ID"}
	}
	if !s.actionMu.TryLock() {
		return HistoryPage{}, &Error{Code: "busy", Message: "another playback operation is running; try again after it finishes"}
	}
	defer s.actionMu.Unlock()
	if ctx.Err() != nil || s.ctx.Err() != nil {
		return HistoryPage{}, &Error{Code: "canceled", Message: "history request canceled or service stopping"}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	page := HistoryPage{Version: StatusVersion, Entries: []HistoryEntry{}}
	if s.state.Cursor >= 0 {
		page.CurrentID = s.state.History[s.state.Cursor].ID
	}
	end := len(s.state.History)
	if before != "" {
		end = -1
		for i, e := range s.state.History {
			if e.ID == before {
				end = i
				break
			}
		}
		if end < 0 {
			return page, &Error{Code: "history_not_found", Message: "history entry was pruned or does not exist; reload history"}
		}
	}
	config, _, configErr := LoadConfig(s.request.ConfigPath)
	resolveRules := s.sourceRuleResolver(ctx, config)
	for i := end - 1; i >= 0 && len(page.Entries) < limit; i-- {
		if err := ctx.Err(); err != nil {
			return HistoryPage{}, err
		}
		e := s.state.History[i]
		rules, ratio, resolution := resolveRules(e.Identity)
		candidate, err := s.store.LookupCandidate(ctx, e.Identity)
		if err != nil {
			return HistoryPage{}, &Error{Code: "history_failed", Message: "could not read history origin"}
		}
		origin := imageOrigin(e.Identity, candidate)
		item := HistoryEntry{
			ID: e.ID, ImageID: e.Identity.ImageID, SourceID: e.SourceID, AcceptedAt: e.AcceptedAt, Current: i == s.state.Cursor,
			Provider: origin.Provider, Image: origin.Image, OriginalPath: origin.OriginalPath, Attribution: origin.Attribution,
			Availability: "unverified", Reason: "recheck_on_replay",
		}
		known, hasMetadata, err := s.store.KnownCachedImage(ctx, e.Identity.ImageID)
		if err != nil {
			return HistoryPage{}, &Error{Code: "history_failed", Message: "could not read image metadata"}
		}
		hash := ""
		if hasMetadata {
			hash = known.Hash
			item.Dimensions = &source.Dimensions{Width: known.Width, Height: known.Height}
		}
		_, present, cacheErr := s.store.CachedImage(ctx, e.Identity.ImageID)
		if cacheErr == nil && present {
			item.Availability = "cached"
		}

		switch {
		case configErr != nil:
			item.Availability, item.Reason = "unavailable", "configuration_invalid"
		case resolution.State != "resolved":
			item.Availability, item.Reason = "unavailable", resolution.Reason
		case enabledHistoryOrigin(e.Identity, candidate, config) != nil:
			item.Availability, item.Reason = "unavailable", "outside_enabled_inputs"
		case e.Identity.ProviderID == omarchyProvider && !s.isActiveThemeHistory(e.Identity):
			item.Availability, item.Reason = "unavailable", "outside_active_theme"
		case s.exclusions.Contains(e.Identity.ImageID, hash):
			item.Availability, item.Reason = "unavailable", "excluded"
		default:
			if present && hasMetadata && item.Dimensions != nil {
				image := ImageInfo{Width: item.Dimensions.Width, Height: item.Dimensions.Height}
				if !rules.matches(image, ratio) {
					item.Availability, item.Reason = "unavailable", "filter_mismatch"
				}
			}
			if item.Availability == "unverified" && item.OriginalPath != "" && e.Identity.ProviderID != omarchyProvider {
				if _, err := localOriginalPath(e.Identity, *candidate.Content, config); err != nil {
					item.Availability, item.Reason = "unavailable", "original_unavailable"
				}
			}
		}
		page.Entries = append(page.Entries, item)
	}
	if end > len(page.Entries) && len(page.Entries) > 0 {
		page.NextBefore = page.Entries[len(page.Entries)-1].ID
	}
	return page, nil
}

func (s *Service) ReplayHistory(ctx context.Context, id string) (Status, error) {
	if !ValidHistoryReference(id) {
		return s.Status(), &Error{Code: "invalid_request", Message: "provide a history ID from panora history ls"}
	}
	return s.perform(ctx, "replay", actionOptions{historyID: id})
}

func (s *Service) replayHistory(ctx context.Context, id string) *Error {
	for i, e := range s.state.History {
		if e.ID != id {
			continue
		}
		config, _, err := LoadConfig(s.request.ConfigPath)
		if err != nil {
			return invalidSavedConfiguration()
		}
		choice, err := s.materializeChoice(ctx, playbackChoice{identity: e.Identity, sourceID: e.SourceID, historyIndex: i}, config)
		if err != nil {
			if errors.Is(err, errExcluded) {
				return &Error{Code: "history_excluded", Message: "image is excluded; restore its exclusion before replay"}
			}
			return &Error{Code: "history_unavailable", Message: "history image cannot play with current inputs, files and image rules; reload history for availability"}
		}
		return s.applyChoice(ctx, choice)
	}
	return &Error{Code: "history_not_found", Message: "history entry was pruned or does not exist; reload history"}
}
