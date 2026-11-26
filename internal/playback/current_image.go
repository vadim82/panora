package playback

import (
	"context"

	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/omarchytheme"
	"github.com/vadim82/panora/internal/source"
)

// CurrentImage describes the accepted entry in the same status snapshot.
// It carries recorded provenance and decoded dimensions without inspecting the desktop or original.
type CurrentImage struct {
	Provider     string              `json:"provider"`
	ProviderName string              `json:"provider_name,omitempty"`
	SourceName   string              `json:"source_name,omitempty"`
	Image        string              `json:"image"`
	OriginalPath string              `json:"original_path,omitempty"`
	Dimensions   *source.Dimensions  `json:"dimensions,omitempty"`
	Attribution  *source.Attribution `json:"attribution,omitempty"`
}

func imageOrigin(identity catalog.Identity, candidate source.Candidate) CurrentImage {
	image := CurrentImage{Provider: identity.ProviderID, Image: candidate.ID, Attribution: candidate.Attribution}
	if identity.ProviderID == omarchyProvider {
		image.SourceName = (omarchytheme.Active{ID: identity.Revision}).Label()
	}
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
	image := imageOrigin(entry.Identity, candidate)
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

type sourceNameKey struct {
	provider string
	input    string
}

func configuredSourceNames(config Config) map[sourceNameKey]string {
	names := make(map[sourceNameKey]string)
	for _, provider := range config.Providers {
		for _, folder := range provider.Folders {
			names[sourceNameKey{provider.ID, folder.ID}] = folder.label()
		}
		for _, search := range provider.Searches {
			names[sourceNameKey{provider.ID, search.ID}] = search.label()
		}
		for _, input := range provider.Inputs {
			names[sourceNameKey{provider.ID, input.ID}] = input.Name
		}
	}
	return names
}
