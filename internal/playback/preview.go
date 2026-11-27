package playback

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/vadim82/panora/internal/cache"
	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/source"
)

// Base64 expands at most 2 MiB of PNG data to less than 3 MiB with the envelope.
const (
	MaxPreviewMessageBytes = 3 << 20
	// The preview API exposes these bounds to transport-only clients.
	MaxPreviewBytes     = cache.MaxPreviewBytes
	MaxPreviewDimension = cache.MaxPreviewDimension
)

type ImagePreview struct {
	Version         int    `json:"version"`
	ImageID         int64  `json:"image_id"`
	Available       bool   `json:"available"`
	Reason          string `json:"reason,omitempty"`
	Width           int    `json:"width,omitempty"`
	Height          int    `json:"height,omitempty"`
	ContentRevision string `json:"content_revision,omitempty"`
	MIMEType        string `json:"mime_type,omitempty"`
	Data            []byte `json:"data,omitempty"`
}

// The accepted path records the hash of the bytes submitted to the desktop.
func previewContentRevision(path string) string {
	name := filepath.Base(path)
	hash := strings.TrimSuffix(name, filepath.Ext(name))
	if len(hash) == 64 {
		if _, err := hex.DecodeString(hash); err == nil {
			return hash
		}
	}
	return ""
}

// Preview serves validated cached bytes independently of playback eligibility,
// so disabled/excluded history can still be inspected. It never calls providers,
// reads originals, changes history or sends a desktop request.
func (s *Service) Preview(ctx context.Context, imageID int64) (ImagePreview, error) {
	if imageID < 1 {
		return ImagePreview{}, &Error{Code: "invalid_request", Message: "provide a positive image ID from history"}
	}
	ctx, cancel := context.WithTimeout(ctx, cache.PreviewTimeout)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	if s.ctx.Err() != nil {
		cancel()
	}
	if err := ctx.Err(); err != nil {
		return ImagePreview{}, previewError(err)
	}
	exists, err := s.store.ImageExists(ctx, imageID)
	if err != nil {
		return ImagePreview{}, previewError(err)
	}
	if !exists {
		return ImagePreview{}, &Error{Code: "preview_not_found", Message: "image does not exist; reload history"}
	}
	result := ImagePreview{Version: StatusVersion, ImageID: imageID, Reason: "not_cached"}
	known, present, err := s.store.KnownCachedImage(ctx, imageID)
	if err != nil {
		return ImagePreview{}, previewError(err)
	}
	if !present {
		return result, nil
	}
	thumbnail, err := s.cache.Preview(ctx, cache.Result{Path: known.Path, Hash: known.Hash, Width: known.Width, Height: known.Height, Bytes: known.Bytes})
	if errors.Is(err, cache.ErrPreviewUnavailable) {
		result.Reason = "content_unavailable"
		return result, nil
	}
	if err != nil {
		return ImagePreview{}, previewError(err)
	}
	result.Available, result.Reason = true, ""
	result.ContentRevision = known.Hash
	result.Width, result.Height, result.MIMEType, result.Data = thumbnail.Width, thumbnail.Height, "image/png", thumbnail.Data
	return result, nil
}

func previewError(err error) *Error {
	switch {
	case errors.Is(err, context.Canceled):
		return &Error{Code: "canceled", Message: "image preview canceled or service stopping"}
	case errors.Is(err, context.DeadlineExceeded):
		return &Error{Code: "timeout", Message: "image preview timed out"}
	case errors.Is(err, cache.ErrPreviewBusy):
		return &Error{Code: "busy", Message: "playback cache is busy; request the preview again after it finishes"}
	default:
		return &Error{Code: "preview_failed", Message: "could not read image preview"}
	}
}

// PreviewNeighbor limits Now thumbnails to adjacent entries that pass the
// known eligibility checks. It inspects only committed metadata, saved rules
// and cached bytes. Playback can still skip an entry whose content changes.
func (s *Service) PreviewNeighbor(ctx context.Context, imageID int64, historyID string) (ImagePreview, error) {
	if !ValidHistoryReference(historyID) || imageID < 1 {
		return ImagePreview{}, &Error{Code: "invalid_request", Message: "provide a neighboring history ID and its image ID"}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	unavailable := ImagePreview{Version: StatusVersion, ImageID: imageID, Reason: "history_unavailable"}

	before := s.neighborPreviewSnapshot(imageID, historyID)
	if before.identity.ImageID == 0 {
		return unavailable, nil
	}
	config, _, saved, err := ReadConfig(s.request.ConfigPath)
	if err != nil {
		return unavailable, nil
	}
	eligible, err := s.neighborPreviewEligible(ctx, before, config)
	if err != nil || !eligible {
		return unavailable, err
	}

	result, err := s.Preview(ctx, imageID)
	if err != nil || !result.Available {
		return result, err
	}
	_, _, latest, err := ReadConfig(s.request.ConfigPath)
	if err != nil || !bytes.Equal(saved, latest) {
		return unavailable, nil
	}
	after := s.neighborPreviewSnapshot(imageID, historyID)
	if after.identity != before.identity || after.currentID != before.currentID ||
		after.exclusionRevision != before.exclusionRevision || after.backend != before.backend ||
		before.identity.ProviderID == omarchyProvider && !s.observedThemeHistory(before.identity) {
		return unavailable, nil
	}
	if err := ctx.Err(); err != nil {
		return ImagePreview{}, previewError(err)
	}
	return result, nil
}

func (s *Service) neighborPreviewEligible(ctx context.Context, snapshot neighborPreviewSnapshot, config Config) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, previewError(err)
	}
	identity := snapshot.identity
	candidate, err := s.store.LookupCandidate(ctx, identity)
	if err != nil {
		return false, previewError(err)
	}
	originEnabled := enabledHistoryOrigin(identity, candidate, config) == nil
	if !originEnabled ||
		source.ValidInputProviderID(identity.ProviderID) && !s.request.inputProviderAvailable(identity.ProviderID) ||
		identity.ProviderID == omarchyProvider && !s.observedThemeHistory(identity) {
		return false, nil
	}
	known, present, err := s.store.KnownCachedImage(ctx, identity.ImageID)
	if err != nil {
		return false, previewError(err)
	}
	if !present || snapshot.exclusions.Contains(identity.ImageID, known.Hash) {
		return false, nil
	}
	rules, ratio, resolution := s.resolveSourceRulesForBackend(ctx, config, identity, snapshot.backend)
	if err := ctx.Err(); err != nil {
		return false, previewError(err)
	}
	return resolution.State == "resolved" &&
		rules.matches(ImageInfo{Width: known.Width, Height: known.Height}, ratio), nil
}

type neighborPreviewSnapshot struct {
	identity          catalog.Identity
	exclusions        catalog.ExclusionSet
	currentID         string
	exclusionRevision uint64
	backend           string
}

type neighborPreviewEntry struct {
	id       string
	identity catalog.Identity
}

// Published membership and exclusions are immutable. They allow visible slots
// to validate eligibility while an apply owns the playback action lock.
func (s *Service) neighborPreviewSnapshot(imageID int64, historyID string) neighborPreviewSnapshot {
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()
	status := &s.status
	for _, entry := range status.previewNeighbors {
		if entry.id == historyID && entry.identity.ImageID == imageID {
			return neighborPreviewSnapshot{
				identity:          entry.identity,
				exclusions:        status.previewExclusions,
				currentID:         status.Current.ID,
				exclusionRevision: status.HistoryPreviewRevision,
				backend:           status.Backend,
			}
		}
	}
	return neighborPreviewSnapshot{}
}

func (s *Service) observedThemeHistory(identity catalog.Identity) bool {
	collection := s.theme.collection.Load()
	observation := s.theme.observation.Load()
	if collection == nil || observation == nil || observation.active != collection.active ||
		identity.InputID != collection.sourceID || identity.Revision != collection.active.ID {
		return false
	}
	for _, still := range collection.stills {
		if still.candidate.Identity.ImageID == identity.ImageID {
			return true
		}
	}
	return false
}
