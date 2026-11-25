package playback

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/vadim82/panora/internal/cache"
	"github.com/vadim82/panora/internal/catalog"
)

const RulePreviewTimeout = 2 * time.Second
const RulePreviewFileBytes int64 = 32 << 20
const RulePreviewPixels = 64_000_000

type RulePreviewRequest struct {
	ProviderID string     `json:"provider_id,omitempty"`
	InputID    string     `json:"input_id,omitempty"`
	Revision   string     `json:"revision"`
	Rules      ImageRules `json:"rules"`
}

type RulePreviewReasons struct {
	BelowMinimum     int `json:"below_minimum"`
	RatioMismatch    int `json:"ratio_mismatch"`
	UnavailableBytes int `json:"unavailable_bytes"`
	UnknownRemote    int `json:"unknown_remote"`
	InspectionBudget int `json:"inspection_budget"`
	OutsideScope     int `json:"outside_scope"`
}

type RulePreview struct {
	ProviderID        string             `json:"provider_id,omitempty"`
	InputID           string             `json:"input_id,omitempty"`
	Version           int                `json:"version"`
	Revision          string             `json:"revision"`
	Rules             ImageRules         `json:"rules"`
	RuleResolution    RuleResolution     `json:"rule_resolution"`
	Scope             string             `json:"scope"`
	Validation        string             `json:"validation"`
	Limit             int                `json:"limit"`
	Inspected         int                `json:"inspected"`
	Matched           int                `json:"matched"`
	Rejected          int                `json:"rejected"`
	Unknown           int                `json:"unknown"`
	Complete          bool               `json:"complete"`
	ExclusionsApplied bool               `json:"exclusions_applied"`
	Reasons           RulePreviewReasons `json:"reasons"`
	Reason            string             `json:"reason,omitempty"`
}

// PreviewRules inspects a draft against a bounded snapshot of discovered images.
// Local headers and currently validated cache bytes are evidence; remote metadata
// and retained dimensions are never counted as proven matches. This endpoint
// neither invokes providers nor publishes files, settings, history or cache use.
func (s *Service) PreviewRules(ctx context.Context, request RulePreviewRequest) (RulePreview, error) {
	if err := ctx.Err(); err != nil {
		return RulePreview{}, previewError(err)
	}
	if (request.ProviderID == "") != (request.InputID == "") || len(request.ProviderID) > 128 || len(request.InputID) > 128 {
		return RulePreview{}, &Error{Code: "invalid_request", Message: "provide both provider_id and input_id for a source preview"}
	}
	if request.Revision == "" {
		return RulePreview{}, &Error{Code: "invalid_request", Message: "provide the reviewed settings revision"}
	}
	request.Rules.RatioPresets = append([]string{}, request.Rules.RatioPresets...)
	rules := SourceImageRules{MinWidth: request.Rules.MinWidth, MinHeight: request.Rules.MinHeight, Ratio: request.Rules.Ratio, RatioPresets: request.Rules.RatioPresets, ExactResolutions: request.Rules.ExactResolutions}
	if validateSourceRules(&rules) != nil || request.Rules.MaxDepth < 0 || request.ProviderID == "" && len(rules.ExactResolutions) > 0 {
		return RulePreview{}, &Error{Code: "invalid_settings", Message: "provide positive minimum dimensions, nonnegative depth and a valid image ratio rule"}
	}
	if pixels := request.Rules.RatioTolerancePixels; pixels != nil && !validRatioTolerance(*pixels) {
		return RulePreview{}, &Error{Code: "invalid_settings", Message: ratioToleranceError}
	}
	operation, cancel := context.WithTimeout(ctx, RulePreviewTimeout)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	if s.ctx.Err() != nil {
		return RulePreview{}, previewError(context.Canceled)
	}
	if !s.configMu.TryRLock() {
		return RulePreview{}, &Error{Code: "busy", Message: "settings are being saved; preview the draft after the save finishes"}
	}
	config, _, data, err := ReadConfig(s.request.ConfigPath)
	s.statusMu.RLock()
	backend := s.effectiveSettings.Backend
	s.statusMu.RUnlock()
	s.configMu.RUnlock()
	if err != nil {
		return RulePreview{}, invalidSavedConfiguration()
	}
	if configRevision(data) != request.Revision {
		return RulePreview{}, &Error{Code: "config_conflict", Message: "settings changed; reload them before previewing this draft"}
	}
	tolerance := config.RatioTolerancePixels()
	if pixels := request.Rules.RatioTolerancePixels; pixels != nil {
		tolerance = *pixels
	}
	ratio, _ := parseRatioRule(rules.Ratio, rules.RatioPresets, tolerance)
	result := RulePreview{Version: StatusVersion, Revision: request.Revision, Rules: request.Rules, Scope: "inherited_catalog", Validation: "local_headers_and_validated_cache", Limit: catalog.MaxRulePreviewImages, Complete: true}
	partial := func(reason string) { result.Complete = false; result.Reason = reason }
	scoped := request.ProviderID != ""
	inputs := []catalog.InputRevision{}
	for _, provider := range config.Providers {
		if !provider.Enabled {
			continue
		}
		for _, input := range providerInputs(provider, config.Filters.MaxDepth, sourceDefaultRules(config)) {
			if input.disabled || !scoped && !input.inherited || scoped && (provider.ID != request.ProviderID || input.id != request.InputID) {
				continue
			}
			if len(inputs) == catalog.MaxRulePreviewInputs {
				partial("input_limit")
				break
			}
			inputs = append(inputs, catalog.InputRevision{ProviderID: provider.ID, InputID: input.id, Revision: input.revision})
		}
	}
	currentDepth := config.Filters.MaxDepth
	if scoped {
		result.ProviderID, result.InputID, result.Scope = request.ProviderID, request.InputID, "source_catalog"
		if _, enabled := configuredInputRules(config, request.ProviderID, request.InputID); !enabled {
			partial("source_not_enabled_or_enrolled")
			return result, nil
		}
		for pi := range config.Providers {
			provider := &config.Providers[pi]
			if provider.ID != request.ProviderID {
				continue
			}
			for fi := range provider.Folders {
				folder := &provider.Folders[fi]
				if folder.ID == request.InputID {
					currentDepth = folder.depth(config.Filters.MaxDepth)
					folder.MaxDepth = &request.Rules.MaxDepth
				}
			}
		}
	}

	if request.ProviderID != omarchyProvider && currentDepth != request.Rules.MaxDepth {
		partial("draft_depth_differs")
	}
	config.Filters.MaxDepth, config.Filters.MinWidth, config.Filters.MinHeight = request.Rules.MaxDepth, request.Rules.MinWidth, request.Rules.MinHeight
	config.Filters.Ratio, config.Filters.RatioPresets = request.Rules.Ratio, request.Rules.RatioPresets
	status := s.Status()
	if previewDiscoveryIncomplete(status, request.ProviderID, request.InputID) {
		partial("discovery_incomplete")
	}
	ratio, result.RuleResolution = s.resolveRatioRule(operation, ratio, backend)
	if ctx.Err() != nil || s.ctx.Err() != nil {
		return RulePreview{}, previewError(context.Canceled)
	}
	if result.RuleResolution.State != "resolved" {
		partial(result.RuleResolution.Reason)
		if operation.Err() != nil {
			partial("time_budget")
		}
		return result, nil
	}
	if request.ProviderID == omarchyProvider {
		s.addOmarchyRulePreview(&result, config, rules, ratio)
		return result, nil
	}
	rows, more, err := s.store.RulePreviewCandidates(operation, inputs)
	if err != nil {
		if ctx.Err() != nil || s.ctx.Err() != nil {
			return RulePreview{}, previewError(context.Canceled)
		}
		if operation.Err() != nil {
			partial("time_budget")
			return result, nil
		}
		return RulePreview{}, &Error{Code: "preview_failed", Message: "could not read discovered images for the rule preview"}
	}
	if more {
		partial("image_limit")
	}
	complete, err := s.store.RulePreviewDiscoveryComplete(operation, inputs)
	if err != nil {
		if ctx.Err() != nil || s.ctx.Err() != nil {
			return RulePreview{}, previewError(context.Canceled)
		}
		if operation.Err() != nil {
			partial("time_budget")
			return result, nil
		}
		return RulePreview{}, &Error{Code: "preview_failed", Message: "could not inspect discovery coverage"}
	}
	if !complete {
		partial("discovery_incomplete")
	}

	fileBytes, pixels := RulePreviewFileBytes, RulePreviewPixels
	for _, row := range rows {
		if ctx.Err() != nil || s.ctx.Err() != nil {
			return RulePreview{}, previewError(context.Canceled)
		}
		if operation.Err() != nil {
			partial("time_budget")
			break
		}
		result.Inspected++
		if enabledHistoryOrigin(row.Identity, row.Candidate, config) != nil {
			result.Rejected++
			result.Reasons.OutsideScope++
			continue
		}
		var image ImageInfo
		known, present, err := s.store.KnownCachedImage(operation, row.Identity.ImageID)
		if err != nil {
			if operation.Err() != nil {
				result.Unknown++
				result.Reasons.InspectionBudget++
				partial("time_budget")
				break
			}
			return RulePreview{}, &Error{Code: "preview_failed", Message: "could not inspect cached-image records"}
		}
		if present {
			if known.Bytes > fileBytes || known.Bytes < 1 || known.Width < 1 || known.Height < 1 || known.Width > pixels/known.Height {
				result.Unknown++
				result.Reasons.InspectionBudget++
				partial("file_budget")
				continue
			}
			fileBytes -= known.Bytes
			pixels -= known.Width * known.Height
			err = s.cache.Validate(operation, cache.Result{Path: known.Path, Hash: known.Hash, Width: known.Width, Height: known.Height, Bytes: known.Bytes})
			if err == nil {
				image = ImageInfo{Width: known.Width, Height: known.Height}
			} else if errors.Is(err, cache.ErrPreviewBusy) {
				partial("cache_busy")
			}
		}
		budgetSkipped := false
		if image.Width == 0 && row.Identity.ProviderID == "local" && row.Candidate.Content != nil {
			path, pathErr := localOriginalPath(row.Identity, *row.Candidate.Content, config)
			if pathErr == nil {
				info, statErr := os.Stat(path)
				if statErr == nil && info.Mode().IsRegular() && info.Size() <= fileBytes {
					fileBytes -= info.Size()
					var headerErr error
					image, headerErr = readImageInfoLimited(operation, path, info.Size())
					if errors.Is(headerErr, errImageHeaderBudget) {
						budgetSkipped = true
						result.Reasons.InspectionBudget++
						partial("file_budget")
					}
				} else if statErr == nil && info.Size() > fileBytes {
					budgetSkipped = true
					result.Reasons.InspectionBudget++
					partial("file_budget")
				}
			}
		}
		if image.Width == 0 {
			result.Unknown++
			if budgetSkipped {
				continue
			} else if !present && row.Identity.ProviderID != "local" {
				result.Reasons.UnknownRemote++
			} else {
				result.Reasons.UnavailableBytes++
			}
			continue
		}
		resolutionMatches := rules.matchesResolution(image)
		matches := ratio.matches(image)
		if resolutionMatches && matches {
			result.Matched++
		} else {
			result.Rejected++
			if !resolutionMatches {
				result.Reasons.BelowMinimum++
			}
			if !matches {
				result.Reasons.RatioMismatch++
			}
		}
	}
	if !scoped && config.OmarchyTheme != nil && config.OmarchyTheme.Enabled && config.OmarchyTheme.ImageRules == nil && operation.Err() == nil {
		s.addOmarchyRulePreview(&result, config, rules, ratio)
	}
	if ctx.Err() != nil || s.ctx.Err() != nil {
		return RulePreview{}, previewError(context.Canceled)
	}
	if operation.Err() != nil {
		partial("time_budget")
	}
	if previewDiscoveryIncomplete(s.Status(), request.ProviderID, request.InputID) {
		partial("discovery_incomplete")
	}
	return result, nil
}

func discoveryIncomplete(status Status) bool {
	for _, provider := range status.Providers {
		for _, input := range provider.Inputs {
			if input.State != "ready" && input.State != "disabled" {
				return true
			}
		}
	}
	return false
}

func previewDiscoveryIncomplete(status Status, providerID, inputID string) bool {
	if providerID == "" {
		return status.Refreshing || discoveryIncomplete(status)
	}
	for _, p := range status.Providers {
		if p.ID != providerID {
			continue
		}
		for _, input := range p.Inputs {
			if input.ID == inputID {
				return input.State != "ready" && input.State != "empty"
			}
		}
	}
	return false
}
