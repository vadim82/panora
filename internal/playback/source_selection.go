package playback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/vadim82/panora/internal/cache"
	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/localfiles"
	"github.com/vadim82/panora/internal/source"
)

const maxDiscoveryPages = 1000
const maxSelectionAttempts = 32
const maxCatalogCandidates = 100000

type inputPool []catalog.Candidate
type providerPool struct {
	id     string
	inputs []inputPool
}

// drawCandidate gives each nonempty provider and then each input equal weight.
// A failing provider gets at most one attempt per round before healthy fallback.
func drawCandidate(pools []providerPool, blocked map[string]bool, draw func(int) int) (catalog.Candidate, bool) {
	available := []int{}
	for i, p := range pools {
		if blocked[p.id] {
			continue
		}
		for _, input := range p.inputs {
			if len(input) > 0 {
				available = append(available, i)
				break
			}
		}
	}
	if len(available) == 0 && len(blocked) > 0 {
		clear(blocked)
		return drawCandidate(pools, blocked, draw)
	}
	if len(available) == 0 {
		return catalog.Candidate{}, false
	}
	pi := available[draw(len(available))]
	inputs := []int{}
	for i, input := range pools[pi].inputs {
		if len(input) > 0 {
			inputs = append(inputs, i)
		}
	}
	ii := inputs[draw(len(inputs))]
	candidates := pools[pi].inputs[ii]
	ci := draw(len(candidates))
	candidate := candidates[ci]
	pools[pi].inputs[ii] = append(candidates[:ci], candidates[ci+1:]...)
	blocked[pools[pi].id] = true
	return candidate, true
}

func filterPools(pools []providerPool, keep func(catalog.Candidate) bool) []providerPool {
	result := []providerPool{}
	for _, p := range pools {
		next := providerPool{id: p.id}
		for _, input := range p.inputs {
			var group inputPool
			for _, c := range input {
				if keep(c) {
					group = append(group, c)
				}
			}
			if len(group) > 0 {
				next.inputs = append(next.inputs, group)
			}
		}
		if len(next.inputs) > 0 {
			result = append(result, next)
		}
	}
	return result
}

type providerInput struct {
	id, scope, revision string
	config              json.RawMessage
	disabled            bool
	inherited           bool
	rules               *SourceImageRules
}

func providerInputs(p ProviderConfig, maxDepth int, defaults SourceImageRules) []providerInput {
	inputs := make([]providerInput, 0, len(p.Folders)+len(p.Searches)+len(p.Inputs))
	rules := func(configured *SourceImageRules) *SourceImageRules {
		if configured != nil {
			return configured
		}
		return &defaults
	}
	for _, folder := range p.Folders {
		settings := source.LocalConfig{Root: folder.Path, MaxDepth: folder.depth(maxDepth)}
		effective := rules(folder.ImageRules)
		revision := inputDiscoveryRevision(p.ID, folder.ID, settings, effective)
		settings.Revision = revision
		raw, _ := json.Marshal(settings)
		inputs = append(inputs, providerInput{inherited: folder.ImageRules == nil, rules: effective, id: folder.ID, scope: localImageScope(folder), revision: revision, config: raw, disabled: !folder.enabled()})
	}
	for _, search := range p.Searches {
		settings := search.settings("")
		effective := rules(search.ImageRules)
		revision := inputDiscoveryRevision(p.ID, search.ID, settings, effective)
		settings.Revision = revision
		raw, _ := json.Marshal(settings)
		inputs = append(inputs, providerInput{inherited: search.ImageRules == nil, rules: effective, id: search.ID, revision: revision, config: raw, disabled: !search.enabled()})
	}
	for _, input := range p.Inputs {
		settings := json.RawMessage(input.Values)
		effective := rules(input.ImageRules)
		revision := inputDiscoveryRevision(p.ID, input.ID, settings, effective)
		inputs = append(inputs, providerInput{inherited: input.ImageRules == nil, rules: effective, id: input.ID, revision: revision, config: settings, disabled: !input.enabled()})
	}
	return inputs
}

func inputDiscoveryRevision(provider, input string, settings any, rules *SourceImageRules) string {
	normalized := *rules
	normalized.RatioPresets = append([]string{}, rules.RatioPresets...)
	data, _ := json.Marshal(struct {
		Provider, Input string
		Settings        any
		Rules           SourceImageRules
	}{provider, input, settings, normalized})
	digest := sha256.Sum256(data)
	return "input-v1-" + hex.EncodeToString(digest[:])
}

// InputRevision identifies the discovery settings of a configured input.
// Disabled inputs keep their revision so they can reuse discovery on re-enable.
func (c Config) InputRevision(providerID, inputID string) string {
	for _, p := range c.Providers {
		if p.ID != providerID {
			continue
		}
		for _, input := range providerInputs(p, c.Filters.MaxDepth, sourceDefaultRules(c)) {
			if input.id == inputID {
				return input.revision
			}
		}
	}
	return ""
}

func legacyInputRevisions(config Config) []catalog.InputRevisionUpgrade {
	var upgrades []catalog.InputRevisionUpgrade
	for _, p := range config.Providers {
		for _, input := range providerInputs(p, config.Filters.MaxDepth, sourceDefaultRules(config)) {
			upgrades = append(upgrades, catalog.InputRevisionUpgrade{
				InputRevision:  catalog.InputRevision{ProviderID: p.ID, InputID: input.id, Revision: input.revision},
				LegacyRevision: p.legacyRevision,
			})
		}
	}
	return upgrades
}

func matchesKnownDimensions(c catalog.Candidate, rules SourceImageRules, ratio *RatioRule) bool {
	dimensions := &c.DecodedDimensions
	if dimensions.Width == 0 || dimensions.Height == 0 {
		dimensions = c.Candidate.Dimensions
	}
	return dimensions == nil || rules.matches(ImageInfo{Width: dimensions.Width, Height: dimensions.Height}, ratio)
}

func (s *Service) candidatePools(ctx context.Context, config Config, budget int, warn func(string)) ([]providerPool, error) {
	return s.selectionPools(ctx, config, budget, warn, s.selectionContext())
}

func (s *Service) selectionPools(ctx context.Context, config Config, budget int, warn func(string), selection selectionContext) ([]providerPool, error) {
	pools := []providerPool{}
	remaining := budget
	shuffle := config.Playback.RotationMode == rotationShuffleAllImages
	resolveRules := s.sourceRuleResolverForBackend(ctx, config, selection.backend)
	for _, p := range config.Providers {
		if !p.Enabled || source.ValidInputProviderID(p.ID) && !s.request.inputProviderAvailable(p.ID) {
			continue
		}
		filterRemoteDimensions := shuffle || p.ID == "wallhaven" || p.ID == source.AlphaCodersID
		group := providerPool{id: p.ID}
		for _, configuredInput := range providerInputs(p, config.Filters.MaxDepth, sourceDefaultRules(config)) {
			if configuredInput.disabled {
				continue
			}
			id := configuredInput.id
			rules, inputRatio, resolution := resolveRules(catalog.Identity{ProviderID: p.ID, InputID: id})
			if resolution.State != "resolved" {
				warn(p.ID + "/" + id + ": " + resolution.Reason)
				continue
			}
			candidates, err := s.store.Candidates(ctx, p.ID, id, configuredInput.revision, remaining)
			if err != nil {
				return nil, err
			}
			remaining -= len(candidates)
			var input inputPool
			for _, c := range candidates {
				if selection.exclusions.Contains(c.Identity.ImageID, c.Hash) {
					continue
				}
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				if err := enabledHistoryOrigin(c.Identity, c.Candidate, config); err != nil {
					continue
				}
				content := c.Candidate.Content
				// Retained dimensions can outlive an evicted copy of a mutable
				// local original. Let materialization reopen and validate it.
				remote := content == nil || content.Kind == "https"
				if filterRemoteDimensions && remote && !matchesKnownDimensions(c, rules, inputRatio) {
					continue
				}
				if p.ID == "local" && c.Hash == "" {
					if content == nil || content.Kind != "local" {
						continue
					}
					if !matchesKnownDimensions(c, rules, inputRatio) {
						continue
					}
				}
				input = append(input, c)
			}
			if len(input) > 0 {
				group.inputs = append(group.inputs, input)
			}
		}
		if len(group.inputs) > 0 {
			pools = append(pools, group)
		}
	}
	if config.OmarchyTheme != nil && config.OmarchyTheme.Enabled {
		sourceID := config.OmarchyTheme.ID
		collection := s.currentThemeCollection()
		if collection == nil || collection.sourceID != sourceID {
			warn("Omarchy theme: active backgrounds are unavailable or still being checked.")
		} else {
			if collection.warning != "" {
				warn(collection.warning)
			}
			rules, ratio, resolution := resolveRules(catalog.Identity{ProviderID: omarchyProvider, InputID: sourceID})
			var group inputPool
			if resolution.State == "resolved" {
				for _, still := range collection.stills {
					if !selection.exclusions.Contains(still.candidate.Identity.ImageID, still.hash) &&
						rules.matches(ImageInfo{Width: still.thumb.Width, Height: still.thumb.Height}, ratio) {
						group = append(group, still.candidate)
					}
				}
			}
			if len(group) != 0 {
				if len(group) > remaining {
					return nil, catalog.ErrCandidateLimit
				}
				pools = append(pools, providerPool{id: omarchyProvider, inputs: []inputPool{group}})
			} else if len(collection.stills) != 0 {
				warn("No active Omarchy theme still matches the current image rules and exclusions.")
			}
		}
	}
	return pools, nil
}

func (s *Service) selectFresh(ctx context.Context, config Config, warn func(string)) (playbackChoice, *Error) {
	selection := s.selectionContext()
	var current playbackChoice
	if i := s.state.Cursor; i >= 0 && i < len(s.state.History) {
		current.identity = s.state.History[i].Identity
		current.hash, _ = s.store.ContentHash(ctx, current.identity.ImageID)
	}
	return s.selectAlternative(ctx, config, warn, selection, current, playbackChoice{}, true)
}

// The background draw can exclude one additional slot and never repeats the
// accepted image when it runs out of distinct alternatives.
func (s *Service) selectAlternative(ctx context.Context, config Config, warn func(string), selection selectionContext, current, extra playbackChoice, allowRepeat bool) (playbackChoice, *Error) {
	draw := selection.draw
	if draw == nil {
		draw = rand.IntN
	}
	pools, err := s.selectionPools(ctx, config, maxCatalogCandidates, warn, selection)
	if errors.Is(err, catalog.ErrCandidateLimit) {
		return playbackChoice{}, &Error{Code: "catalog_failed", Message: fmt.Sprintf("enabled inputs exceed the %d-candidate selection budget; narrow folders or searches", maxCatalogCandidates)}
	}
	if err != nil {
		return playbackChoice{}, &Error{Code: "catalog_failed", Message: err.Error()}
	}
	if len(pools) == 0 && s.Status().Refreshing {
		return playbackChoice{}, &Error{Code: "discovery_pending", Message: "checking configured inputs; no matching images are ready yet"}
	}
	if len(pools) == 0 && !selection.speculative {
		var inputs [][]catalog.Identity
		for _, p := range config.Providers {
			if !p.Enabled || source.ValidInputProviderID(p.ID) && !s.request.inputProviderAvailable(p.ID) {
				continue
			}
			var choices []catalog.Identity
			for _, input := range providerInputs(p, config.Filters.MaxDepth, sourceDefaultRules(config)) {
				if !input.disabled {
					choices = append(choices, catalog.Identity{ProviderID: p.ID, InputID: input.id, Revision: input.revision})
				}
			}
			if len(choices) > 0 {
				inputs = append(inputs, choices)
			}
		}
		if len(inputs) > 0 {
			chosen := inputs[draw(len(inputs))]
			s.requestDiscovery(chosen[draw(len(chosen))], 0)
		}
	}
	currentID, currentHash := current.identity.ImageID, current.hash
	extraID, extraHash := extra.identity.ImageID, extra.hash
	if extraID > 0 {
		pools = filterPools(pools, func(c catalog.Candidate) bool {
			return c.Identity.ImageID != extraID && (extraHash == "" || c.Hash != extraHash)
		})
	}
	repeats := filterPools(pools, func(c catalog.Candidate) bool {
		return c.Identity.ImageID == currentID || currentHash != "" && c.Hash == currentHash
	})
	pools = filterPools(pools, func(c catalog.Candidate) bool {
		return c.Identity.ImageID != currentID && (currentHash == "" || c.Hash != currentHash) && !queuedCandidate(c, selection.queued)
	})
	counts := candidateCounts(pools)
	drawNext, drawRepeat := freshDraws(config.Playback.RotationMode, pools, repeats, draw)
	var repeat *playbackChoice
	hadFailure := false
	for attempts := 0; attempts < maxSelectionAttempts && ctx.Err() == nil; attempts++ {
		candidate, ok := drawNext()
		if !ok {
			if !allowRepeat {
				break
			}
			if repeat != nil && !hadFailure {
				return *repeat, nil
			}
			if hadFailure {
				break
			}
			candidate, ok = drawRepeat()
			if !ok {
				break
			}
		}
		attemptCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		identity := candidate.Identity
		choice, err := s.materializeSelection(attemptCtx, playbackChoice{identity: identity, sourceID: identity.InputID, historyIndex: -1, candidates: counts[discoveryKey(identity.ProviderID, identity.InputID)]}, config, selection)
		cancel()
		if err != nil {
			var typed *Error
			ruleRejected := errors.As(err, &typed) && typed.Code == "filter_mismatch"
			if !errors.Is(err, errExcluded) && !ruleRejected {
				hadFailure = true
			}
			warn(candidate.Identity.ProviderID + ": " + err.Error())
			continue
		}
		if extraHash != "" && choice.hash == extraHash || queuedCandidate(catalog.Candidate{Identity: choice.identity, Hash: choice.hash}, selection.queued) {
			continue
		}
		if currentHash != "" && choice.hash == currentHash {
			repeat = &choice
			continue
		}
		return choice, nil
	}
	return playbackChoice{}, &Error{Code: "selection_failed", Message: "no available image matches the current inputs and filters within the selection budget"}
}

func candidateCounts(pools []providerPool) map[string]int {
	counts := map[string]int{}
	for _, pool := range pools {
		for _, input := range pool.inputs {
			if len(input) > 0 {
				counts[discoveryKey(pool.id, input[0].Identity.InputID)] = len(input)
			}
		}
	}
	return counts
}

// discoverMetadata commits each page before requesting the next. Interrupted
// discovery resumes its persisted cursor; provider calls never hold SQL writes.
func discoverMetadataWithCommit(ctx context.Context, store *catalog.Store, provider source.Provider, providerID, input, revision, scope string, config json.RawMessage, commit func(context.Context, catalog.Scan, string, source.Page) error, budget int) error {
	var local source.LocalConfig
	headerCtx := ctx
	if providerID == "local" {
		if err := source.Decode(config, &local); err != nil {
			return fmt.Errorf("read local discovery settings: %w", err)
		}
		// Preserve time for the page transaction after best-effort header reads.
		const commitHeadroom = 500 * time.Millisecond
		headerDeadline := time.Now().Add(2500 * time.Millisecond)
		if deadline, ok := ctx.Deadline(); ok && deadline.Add(-commitHeadroom).Before(headerDeadline) {
			headerDeadline = deadline.Add(-commitHeadroom)
		}
		var cancel context.CancelFunc
		headerCtx, cancel = context.WithDeadline(ctx, headerDeadline)
		defer cancel()
	}
	description, err := provider.Describe(ctx)
	if err != nil {
		return err
	}
	if description.Protocol != source.Protocol || description.ID != providerID || len(description.Requirements) != 0 || !slices.Contains(description.MediaKinds, "image") {
		return fmt.Errorf("provider description does not match configured image provider")
	}
	kinds := []string{"https"}
	switch providerID {
	case "local":
		kinds = []string{"local"}
	case "wallhaven":
	default:
		kinds = append(kinds, "local")
	}
	kinds = slices.DeleteFunc(kinds, func(kind string) bool {
		return !slices.Contains(description.ContentKinds, kind)
	})
	if len(kinds) == 0 {
		return fmt.Errorf("provider does not support a configured image content kind")
	}
	for _, operation := range []string{"describe", "validate", "discover", "resolve"} {
		if !slices.Contains(description.Operations, operation) {
			return fmt.Errorf("provider operation unavailable: %s", operation)
		}
	}
	if err := provider.Validate(ctx, config); err != nil {
		return err
	}
	scan, err := store.StartScan(ctx, providerID, input, revision, false)
	if err != nil {
		return err
	}
	restarted := false
	seen := map[[32]byte]bool{}
	for n := 0; n < min(maxDiscoveryPages, budget); n++ {
		page, err := provider.Discover(ctx, source.DiscoverParams{Config: config, Cursor: scan.Cursor, Limit: source.MaxPageSize})
		if err != nil {
			var typed *source.Error
			if !restarted && errors.As(err, &typed) && typed.Code == "invalid_cursor" {
				restarted = true
				scan, err = store.StartScan(ctx, providerID, input, revision, true)
				if err == nil {
					clear(seen)
					continue
				}
			}
			return err
		}
		if len(page.NextCursor) == 0 || !json.Valid(page.NextCursor) || len(page.NextCursor) > source.MaxCursorBytes {
			return fmt.Errorf("invalid discovery cursor")
		}
		if page.Candidates == nil || len(page.Candidates) > source.MaxPageSize {
			return fmt.Errorf("invalid discovery page size")
		}
		hash := sha256.Sum256(page.NextCursor)
		if !source.IsNull(page.NextCursor) && seen[hash] {
			return fmt.Errorf("provider repeated discovery cursor")
		}
		seen[hash] = true
		for i := range page.Candidates {
			candidate := &page.Candidates[i]
			if candidate.Content == nil && !slices.Contains(kinds, "https") || candidate.Content != nil && !slices.Contains(kinds, candidate.Content.Kind) {
				return fmt.Errorf("provider returned unsupported content")
			}
			if providerID == "local" {
				candidate.Dimensions = localDiscoveryDimensions(headerCtx, local, candidate.Content.Path)
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := commit(ctx, scan, scope, page); err != nil {
			return err
		}
		if source.IsNull(page.NextCursor) {
			return nil
		}
		scan.Cursor = page.NextCursor
	}
	if budget < maxDiscoveryPages {
		return fmt.Errorf("discovery exceeded shared page budget")
	}
	return fmt.Errorf("discovery exceeded page budget")
}

func localDiscoveryDimensions(ctx context.Context, config source.LocalConfig, candidatePath string) *source.Dimensions {
	if ctx.Err() != nil {
		return nil
	}
	path, err := localfiles.ContainedPath(config.Root, candidatePath, config.MaxDepth)
	if err != nil {
		return nil
	}
	// Metadata helps selection prefilter, but playback validates selected bytes.
	// Files without known dimensions remain eligible for bounded attempts.
	const maxLocalHeaderBytes = 1 << 20
	info, err := readImageHeader(ctx, path, cache.MaxImageBytes, maxLocalHeaderBytes)
	if err != nil || info.Width < 1 || info.Height < 1 {
		return nil
	}
	return &source.Dimensions{Width: info.Width, Height: info.Height}
}

func refreshInterval(provider, input string) time.Duration {
	value := input
	if value == "" {
		value = provider
	}
	if value == "" {
		value = "5m"
	}
	interval, _ := time.ParseDuration(value)
	return interval
}

// Include the normalized root in local image identity, independently of the
// editable label, depth and stable input ID. No database migration is needed;
// prior memberships/history stay intact and new scans use the qualified scope.
func localImageScope(folder FolderConfig) string {
	root := sha256.Sum256([]byte(folder.Path))
	return folder.ID + "/" + hex.EncodeToString(root[:])
}
