package playback

import (
	"context"
	"errors"
	"time"

	"github.com/vadim82/panora/internal/catalog"
)

const MaxExclusionsPageSize = catalog.MaxExclusionsPageSize

var errExcluded = errors.New("image is excluded; restore it before playback")

type Exclusion struct {
	ID         int64     `json:"id"`
	Provider   string    `json:"provider"`
	Image      string    `json:"image"`
	SourceID   string    `json:"source_id,omitempty"`
	Title      string    `json:"title,omitempty"`
	PageURL    string    `json:"page_url,omitempty"`
	ExcludedAt time.Time `json:"excluded_at"`
}

type ExclusionPage struct {
	Version    int         `json:"version"`
	Entries    []Exclusion `json:"entries"`
	NextBefore int64       `json:"next_before,omitempty"`
}

func (s *Service) Exclusions(ctx context.Context, before int64, limit int) (ExclusionPage, error) {
	if before < 0 || limit < 1 || limit > MaxExclusionsPageSize {
		return ExclusionPage{}, &Error{Code: "invalid_request", Message: "before must be nonnegative and limit must be between 1 and 10"}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, next, err := s.store.ListExclusions(ctx, before, limit)
	if err != nil {
		return ExclusionPage{}, &Error{Code: "exclusions_failed", Message: "could not read exclusions"}
	}
	page := ExclusionPage{Version: StatusVersion, Entries: []Exclusion{}, NextBefore: next}
	for _, row := range rows {
		entry := Exclusion{ID: row.ID, Provider: row.ProviderID, Image: row.ProviderImageID, ExcludedAt: row.ExcludedAt}
		entry.SourceID = row.InputID
		if a := row.Candidate.Attribution; a != nil {
			entry.Title = a.Title
			entry.PageURL = a.PageURL
		}
		page.Entries = append(page.Entries, entry)
	}
	return page, nil
}

func (s *Service) RestoreExclusion(ctx context.Context, id int64) (Status, error) {
	if id <= 0 {
		return s.Status(), &Error{Code: "invalid_request", Message: "exclusion ID must be positive"}
	}
	return s.perform(ctx, "restore-exclusion", actionOptions{exclusionID: id})
}

type currentExclusion struct {
	instance string
	current  HistoryReference
}

// ExcludeCurrent marks only the accepted entry the caller reviewed.
func (s *Service) ExcludeCurrent(ctx context.Context, instance string, current HistoryReference) (Status, error) {
	if instance == "" || len(instance) > 128 || !ValidHistoryReference(current.ID) || current.ImageID <= 0 {
		return s.Status(), &Error{Code: "invalid_request", Message: "provide a service instance, history ID and positive image ID"}
	}
	return s.perform(ctx, "exclude", actionOptions{exclude: &currentExclusion{instance: instance, current: current}})
}

// Called under actionMu so the cursor stays fixed until the mark commits.
func (s *Service) checkExclusionTarget(target currentExclusion) *Error {
	if target.instance != s.instanceID {
		return &Error{Code: "instance_changed", Message: "The Panora service changed before the image could be excluded. Review the current image and try again."}
	}
	if s.state.Cursor >= 0 {
		entry := s.state.History[s.state.Cursor]
		if entry.ID == target.current.ID && entry.Identity.ImageID == target.current.ImageID {
			return nil
		}
	}
	return &Error{Code: "current_changed", Message: "The wallpaper changed before it could be excluded. Review the current image and try again."}
}

// Called with the same action lock as playback. These operations change marks,
// not the desktop, history cursor, pause state or rotation deadline.
func (s *Service) changeExclusion(ctx context.Context, action string, id int64) *Error {
	var set catalog.ExclusionSet
	var err error
	if action == "exclude" {
		if s.state.Pending != nil {
			return &Error{Code: "apply_uncertain", Message: "resolve the uncertain desktop application before excluding the current image"}
		}
		if s.state.Cursor < 0 {
			return &Error{Code: "no_current", Message: "no accepted current image to exclude"}
		}
		set, err = s.store.ExcludeImage(ctx, s.state.History[s.state.Cursor].Identity.ImageID)
	} else {
		set, err = s.store.RestoreExclusion(ctx, id)
	}
	if errors.Is(err, catalog.ErrExclusionNotFound) {
		return &Error{Code: "exclusion_not_found", Message: "no matching active exclusion to restore"}
	}
	if err != nil {
		return &Error{Code: "exclusions_failed", Message: "could not save exclusions"}
	}
	s.exclusions = set
	s.historyPreviewRevision++
	return nil
}
