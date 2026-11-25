package playback

import (
	"context"

	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/source"
)

// CurrentImage describes the accepted entry in the same status snapshot.
// It carries recorded provenance and decoded dimensions without inspecting the desktop or original.
type CurrentImage struct {
	Provider     string              `json:"provider"`
	Image        string              `json:"image"`
	OriginalPath string              `json:"original_path,omitempty"`
	Dimensions   *source.Dimensions  `json:"dimensions,omitempty"`
	Attribution  *source.Attribution `json:"attribution,omitempty"`
}

func imageOrigin(provider string, candidate source.Candidate) CurrentImage {
	image := CurrentImage{Provider: provider, Image: candidate.ID, Attribution: candidate.Attribution}
	if candidate.Content != nil && candidate.Content.Kind == "local" {
		image.OriginalPath = candidate.Content.Path
	}
	return image
}

// Rebuild the projection once on startup from the retained accepted membership.
func loadCurrentImage(ctx context.Context, store *catalog.Store, entry Entry) *CurrentImage {
	candidate, err := store.LookupCandidate(ctx, entry.Identity)
	if err != nil {
		return nil
	}
	image := imageOrigin(entry.Identity.ProviderID, candidate)
	if known, ok, err := store.KnownCachedImage(ctx, entry.Identity.ImageID); err == nil && ok && known.Path == entry.Path {
		image.Dimensions = &source.Dimensions{Width: known.Width, Height: known.Height}
	}
	return &image
}

func (image CurrentImage) clone() *CurrentImage {
	if image.Dimensions != nil {
		dimensions := *image.Dimensions
		image.Dimensions = &dimensions
	}
	if image.Attribution != nil {
		attribution := *image.Attribution
		image.Attribution = &attribution
	}
	return &image
}
