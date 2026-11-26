package playback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vadim82/panora/internal/cache"
	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/localfiles"
	"github.com/vadim82/panora/internal/source"
)

func DefaultCachePath() (string, error) {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" && !filepath.IsAbs(xdg) {
		return "", fmt.Errorf("XDG_CACHE_HOME must be absolute")
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "panora", "playback"), nil
}

func enabledHistoryOrigin(identity catalog.Identity, candidate source.Candidate, config Config) error {
	if identity.ProviderID == omarchyProvider && config.OmarchyTheme != nil && config.OmarchyTheme.ID == identity.InputID && config.OmarchyTheme.Enabled {
		return nil
	}
	for _, p := range config.Providers {
		if !p.Enabled || p.ID != identity.ProviderID {
			continue
		}
		for _, folder := range p.Folders {
			if folder.ID != identity.InputID || !folder.enabled() {
				continue
			}
			if candidate.Content == nil || candidate.Content.Kind != "local" {
				return fmt.Errorf("local history has no original")
			}
			relative, err := filepath.Rel(folder.Path, candidate.Content.Path)
			if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
				return fmt.Errorf("outside the enabled folder")
			}
			if filepath.ToSlash(relative) != candidate.ID {
				return fmt.Errorf("outside the original folder root")
			}
			if strings.Count(relative, string(filepath.Separator)) > folder.depth(config.Filters.MaxDepth) {
				return fmt.Errorf("outside configured scan depth")
			}
			return nil
		}
		for _, input := range p.Searches {
			if input.ID == identity.InputID && input.enabled() {
				return nil
			}
		}
		for _, input := range p.Inputs {
			if input.ID == identity.InputID && input.enabled() {
				if p.ID == source.CommonsID {
					saved, err := source.DecodeCommonsConfig(json.RawMessage(input.Values))
					if err != nil || !saved.Allows(candidate) {
						return fmt.Errorf("Commons image is outside the reviewed source")
					}
				}
				return nil
			}
		}
	}
	return fmt.Errorf("outside enabled provider inputs")
}

func (s *Service) materializeChoice(ctx context.Context, choice playbackChoice, config Config) (playbackChoice, error) {
	return s.materializeSelection(ctx, choice, config, s.selectionContext())
}

func (s *Service) materializeSelection(ctx context.Context, choice playbackChoice, config Config, selection selectionContext) (playbackChoice, error) {
	key := catalog.RetryKey{ProviderID: choice.identity.ProviderID, InputID: choice.identity.InputID, Revision: choice.identity.Revision, Scope: "materialization", ImageID: choice.identity.ImageID}
	result, err := s.materializeImage(ctx, choice, config, key, selection)
	if err == nil {
		if !selection.speculative {
			s.observeRetry(key, "materialization", nil)
		}
		return result, nil
	}
	var deferred *retryDeferred
	if errors.As(err, &deferred) || errors.Is(err, errExcluded) ||
		selection.speculative && (ctx.Err() != nil || errors.Is(err, cache.ErrCapacity)) {
		return result, err
	}
	// A disappeared local original can be probed by the next discovery or
	// requested action. Warmup need not add a competing provider-health error.
	if !selection.speculative || choice.identity.ProviderID != "local" {
		s.observeRetry(key, "materialization", err)
	}
	failure := refreshFailure(atStage("materialization", err))
	return result, &Error{Code: failure.Code, Message: failure.Stage + ": " + failure.Message}
}

func (s *Service) materializeImage(ctx context.Context, choice playbackChoice, config Config, key catalog.RetryKey, selection selectionContext) (playbackChoice, error) {
	if selection.exclusions.Contains(choice.identity.ImageID, "") {
		return choice, errExcluded
	}
	if len(selection.exclusions.Hashes) > 0 {
		hash, err := s.store.ContentHash(ctx, choice.identity.ImageID)
		if err != nil {
			return choice, err
		}
		if selection.exclusions.Contains(choice.identity.ImageID, hash) {
			return choice, errExcluded
		}
	}
	candidate, err := s.store.LookupCandidate(ctx, choice.identity)
	if err != nil {
		return choice, fmt.Errorf("lookup original: %w", err)
	}
	if err := enabledHistoryOrigin(choice.identity, candidate, config); err != nil {
		return choice, err
	}
	if source.SupportsProviderInputs(choice.identity.ProviderID) && s.request.provider(choice.identity.ProviderID) == nil {
		return choice, fmt.Errorf("provider is unavailable")
	}
	rules, ratio, resolution := s.resolveSourceRulesForBackend(ctx, config, choice.identity, selection.backend)
	if resolution.State != "resolved" {
		return choice, unresolvedRuleError(resolution)
	}
	var themeOrigin *themeStill
	if choice.identity.ProviderID == omarchyProvider {
		still, err := s.themeStillFor(choice.identity)
		if err != nil {
			return choice, err
		}
		themeOrigin = &still
	}
	protected := selection.protected
	var result cache.Result
	cached, ok, err := s.store.CachedImage(ctx, choice.identity.ImageID)
	if err != nil {
		return choice, err
	}
	if ok && candidate.Content != nil && candidate.Content.SHA256 != "" && cached.Hash != candidate.Content.SHA256 {
		ok = false
	}
	if ok {
		result, err = s.cache.Reuse(ctx, cache.Result(cached), protected...)
		if selection.speculative && errors.Is(err, cache.ErrCapacity) {
			return choice, err
		}
	}
	if !ok || err != nil {
		if err := s.checkRetry(key, "provider_metadata"); err != nil {
			return choice, err
		}
		var content source.Content
		if themeOrigin != nil {
			content = *themeOrigin.candidate.Candidate.Content
		} else {
			content, err = s.resolveOriginal(ctx, choice.identity, candidate, config)
			if err != nil {
				return choice, atStage("resolve", err)
			}
			s.clearProviderRetry(choice.identity.ProviderID, "provider_metadata")
			if content.Kind == "local" {
				content.Path, err = localOriginalPath(choice.identity, content, config)
				if err != nil {
					return choice, err
				}
			}
		}

		if err := s.checkRetry(key, "provider_download"); err != nil {
			return choice, err
		}
		result, err = s.cache.Materialize(ctx, content, protected...)
		if err != nil {
			return choice, err
		}
		if content.Kind == "https" {
			s.clearProviderRetry(choice.identity.ProviderID, "provider_download")
		}
	}
	if choice.identity.ProviderID == source.CommonsID && candidate.Content != nil && candidate.Content.RevisionSHA1 != "" &&
		(candidate.Dimensions == nil || result.Width != candidate.Dimensions.Width || result.Height != candidate.Dimensions.Height) {
		return choice, atStage("filter", fmt.Errorf("decoded Commons image differs from its reviewed dimensions"))
	}
	if themeOrigin != nil && result.Hash != themeOrigin.hash {
		return choice, fmt.Errorf("Omarchy theme still changed during playback selection")
	}
	record := catalog.CachedFile{Path: result.Path, Hash: result.Hash, Width: result.Width, Height: result.Height, Bytes: result.Bytes}
	if err := s.store.SaveCachedImage(ctx, choice.identity.ImageID, record); err != nil {
		return choice, fmt.Errorf("record cached image: %w", err)
	}
	if selection.exclusions.Contains(choice.identity.ImageID, result.Hash) {
		return choice, errExcluded
	}
	image := ImageInfo{Path: result.Path, Width: result.Width, Height: result.Height}
	if !rules.matches(image, ratio) {
		return choice, atStage("filter", fmt.Errorf("decoded image does not match current image filters"))
	}
	choice.path = result.Path
	choice.hash = result.Hash
	metadata := imageOrigin(choice.identity, candidate)
	metadata.Dimensions = &source.Dimensions{Width: result.Width, Height: result.Height}
	choice.image = &metadata
	return choice, nil
}

func (s *Service) resolveOriginal(ctx context.Context, id catalog.Identity, candidate source.Candidate, config Config) (source.Content, error) {
	provider := s.request.provider(id.ProviderID)
	if provider == nil {
		return source.Content{}, fmt.Errorf("provider unavailable for original resolution")
	}
	var raw []byte
	for _, p := range config.Providers {
		if p.ID != id.ProviderID || !p.Enabled {
			continue
		}
		for _, input := range providerInputs(p, config.Filters.MaxDepth, sourceDefaultRules(config)) {
			if input.id == id.InputID {
				raw = input.config
				break
			}
		}
	}
	resolved, err := provider.Resolve(ctx, source.ResolveParams{Config: raw, ID: candidate.ID, Ref: candidate.Ref})
	if err != nil {
		return source.Content{}, err
	}
	if err := resolved.Validate(); err != nil {
		return source.Content{}, err
	}
	if id.ProviderID == "local" && (candidate.Content == nil || resolved.Content.Kind != "local" || filepath.Clean(resolved.Content.Path) != filepath.Clean(candidate.Content.Path)) {
		return source.Content{}, fmt.Errorf("resolved local original differs from selected image")
	}
	if (id.ProviderID == "wallhaven" || id.ProviderID == source.CommonsID) && resolved.Content.Kind != "https" {
		return source.Content{}, fmt.Errorf("remote provider returned non-HTTPS content")
	}
	if source.ValidExternalID(id.ProviderID) && resolved.Content.Kind == "local" {
		if candidate.Content == nil || candidate.Content.Kind != "local" ||
			filepath.Clean(resolved.Content.Path) != filepath.Clean(candidate.Content.Path) {
			return source.Content{}, fmt.Errorf("resolved provider original differs from discovered image")
		}
	}
	if candidate.Content != nil && (candidate.Content.SHA256 != "" && resolved.Content.SHA256 != candidate.Content.SHA256 ||
		candidate.Content.RevisionSHA1 != "" && (resolved.Content.RevisionSHA1 != candidate.Content.RevisionSHA1 || resolved.Content.MIMEType != candidate.Content.MIMEType)) {
		return source.Content{}, fmt.Errorf("resolved original differs from the expected revision")
	}
	return resolved.Content, nil
}

func localOriginalPath(id catalog.Identity, content source.Content, config Config) (string, error) {
	for _, p := range config.Providers {
		if !p.Enabled || p.ID != id.ProviderID {
			continue
		}
		for _, folder := range p.Folders {
			if folder.ID == id.InputID && folder.enabled() {
				return localfiles.ContainedPath(folder.Path, content.Path, folder.depth(config.Filters.MaxDepth))
			}
		}
		for _, input := range p.Inputs {
			if input.ID == id.InputID && input.enabled() {
				// The explicitly installed provider supplied this absolute path.
				// The cache validates regular files and never owns the original.
				return filepath.Clean(content.Path), nil
			}
		}
	}
	return "", fmt.Errorf("original is outside enabled local inputs")
}
