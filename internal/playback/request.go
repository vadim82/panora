package playback

import (
	"context"

	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/source"
)

type Request struct {
	ConfigPath          string
	StatePath           string
	CachePath           string
	Options             desktop.DisplayOptions
	Source              source.Provider
	Providers           map[string]source.Provider
	ProviderEditors     []ProviderEditor
	InspectCapabilities func(context.Context, string) (desktop.Capabilities, error)
	ResolveBackend      func(context.Context, string) (desktop.WallpaperBackend, error)
}

type Resolver func(context.Context) (desktop.WallpaperBackend, error)

// provider keeps bundled local wiring and configured provider lookup in one place.
// Avoid copying Options while playback updates them under its action lock.
func (r *Request) provider(id string) source.Provider {
	if id == "local" {
		return r.Source
	}
	return r.Providers[id]
}
