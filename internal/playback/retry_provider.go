package playback

import (
	"context"
	"encoding/json"

	"github.com/vadim82/panora/internal/source"
)

// Recheck shared gates between calls/pages, including when another input learns
// a rate limit while this input is in flight. Existing calls are not replayed.
type retryProvider struct {
	source.Provider
	before func() error
}

func (p retryProvider) Describe(ctx context.Context) (source.Description, error) {
	if err := p.before(); err != nil {
		return source.Description{}, err
	}
	return p.Provider.Describe(ctx)
}
func (p retryProvider) Validate(ctx context.Context, config json.RawMessage) error {
	if err := p.before(); err != nil {
		return err
	}
	return p.Provider.Validate(ctx, config)
}
func (p retryProvider) Discover(ctx context.Context, params source.DiscoverParams) (source.Page, error) {
	if err := p.before(); err != nil {
		return source.Page{}, err
	}
	return p.Provider.Discover(ctx, params)
}
func (p retryProvider) Resolve(ctx context.Context, params source.ResolveParams) (source.Resolved, error) {
	if err := p.before(); err != nil {
		return source.Resolved{}, err
	}
	return p.Provider.Resolve(ctx, params)
}
