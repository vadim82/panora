package playback

import (
	"context"
	"fmt"
	"time"

	"github.com/vadim82/panora/internal/desktop"
)

type Capabilities = desktop.Capabilities

// Capabilities inspects the running service's backend choice without holding the
// playback action lock while probing. An in-flight read describes its snapshot.
func (s *Service) Capabilities(ctx context.Context) (Capabilities, error) {
	if err := ctx.Err(); err != nil {
		return Capabilities{}, err
	}
	if s.request.InspectCapabilities == nil {
		return Capabilities{}, &Error{Code: "unsupported", Message: "desktop capability inspection is unavailable"}
	}
	operation, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	if err := s.ctx.Err(); err != nil {
		return Capabilities{}, err
	}
	s.statusMu.RLock()
	backend := s.effectiveSettings.Backend
	s.statusMu.RUnlock()
	result, err := s.request.InspectCapabilities(operation, backend)
	if err != nil {
		return Capabilities{}, fmt.Errorf("inspect desktop capabilities: %w", err)
	}
	return result, nil
}
