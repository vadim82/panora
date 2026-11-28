package playback

import (
	"context"
	"errors"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/vadim82/panora/internal/catalog"
)

const (
	lookaheadSlots = 5
	lookaheadLimit = 10
)

type selectionContext struct {
	exclusions  catalog.ExclusionSet
	backend     string
	protected   []string
	queued      []playbackChoice
	draw        func(int) int
	speculative bool
}

// selectionContext copies the action-owned inputs needed during selection.
func (s *Service) selectionContext() selectionContext {
	return selectionContext{exclusions: s.exclusions, backend: s.backend, protected: s.protectedCachePaths(), draw: s.draw}
}

type lookaheadKey struct {
	configRevision    string
	cursorID          string
	exclusionRevision uint64
	backend           string
	themeID           string
}

type lookaheadState struct {
	revision uint64
	key      lookaheadKey
	choices  []playbackChoice
	planned  []playbackChoice
	cancel   context.CancelFunc
	done     chan struct{}
}

func (s *Service) currentLookaheadKey(saved []byte) lookaheadKey {
	key := lookaheadKey{configRevision: configRevision(saved), backend: s.backend, exclusionRevision: s.historyPreviewRevision}
	if s.state.Cursor >= 0 {
		key.cursorID = s.state.History[s.state.Cursor].ID
	}
	if theme := s.theme.observation.Load(); theme != nil && theme.enabled {
		key.themeID = theme.active.ID
	}
	return key
}

// StartLookahead starts the background cache warmer. It has no rotation timer
// or desktop authority and joins before Close lets go of the catalog and cache.
func (s *Service) StartLookahead() {
	s.discoveryMu.Lock()
	defer s.discoveryMu.Unlock()
	if s.lookaheadDone != nil || s.ctx.Err() != nil {
		return
	}
	s.lookaheadDone = make(chan struct{})
	go s.runLookahead(s.lookaheadDone)
}

func (s *Service) runLookahead(done chan<- struct{}) {
	defer close(done)
	for s.ctx.Err() == nil {
		s.prepareLookahead()
		select {
		case <-s.ctx.Done():
		case <-s.lookaheadWake:
		}
	}
}

func (s *Service) wakeLookahead() {
	select {
	case s.lookaheadWake <- struct{}{}:
	default:
	}
}

func (s *Service) prepareLookahead() {
	config, _, saved, configErr := ReadConfig(s.request.ConfigPath)
	s.actionMu.Lock()
	key := s.currentLookaheadKey(saved)
	slotLimit := min(lookaheadSlots, config.Cache.MaxImages-1)
	state := &s.lookahead
	if key != state.key || len(state.choices) > 0 && (configErr != nil || s.state.Pending != nil) {
		state.key, state.choices, state.planned = key, nil, nil
		state.revision++
		s.publishStatus()
	}
	if s.ctx.Err() != nil || configErr != nil || s.state.Pending != nil || s.state.Cursor < 0 ||
		!config.HasEnabledInputs() || slotLimit < 1 {
		s.actionMu.Unlock()
		return
	}
	selection := s.selectionContext()
	selection.speculative = true
	history := s.state.History
	index := s.state.Cursor
	current := playbackChoice{identity: history[index].Identity, hash: s.currentHash}
	choices := slices.Clone(state.choices)
	planned := slices.Clone(state.planned)
	ctx, cancel := context.WithTimeout(s.ctx, 12*time.Second)
	done := make(chan struct{})
	state.cancel, state.done = cancel, done
	s.actionMu.Unlock()
	defer close(done)
	defer cancel()

	// Recheck retained slots when playback or discovery wakes the queue. Preview verification stays behind
	// the same cache lock as every other read.
	for i, ready := range choices {
		preview, err := s.Preview(ctx, ready.identity.ImageID)
		if err != nil {
			return
		}
		if !preview.Available {
			choices = choices[:i]
			break
		}
	}
	nextIndex := index + 1
	protected := len(selection.protected)
	for len(choices) < slotLimit && ctx.Err() == nil {
		if len(choices) > 0 {
			last := choices[len(choices)-1]
			nextIndex = last.historyIndex + 1
			if last.historyIndex < 0 {
				nextIndex = len(history)
			}
		}
		selection.queued = choices
		selection.protected = selection.protected[:protected]
		for _, ready := range choices {
			selection.protected = append(selection.protected, ready.path)
		}
		choice := s.prepareNext(ctx, config, history, nextIndex, selection, current, &planned)
		if choice.identity.ImageID == 0 {
			break
		}
		choices = append(choices, choice)
		if !s.publishLookahead(ctx, key, choices, planned) {
			return
		}
	}
	if len(choices) > 0 && (choices[len(choices)-1].historyIndex < 0 || choices[len(choices)-1].historyIndex == len(history)-1) {
		planned = s.planLookahead(ctx, config, selection, current, choices, planned)
	}
	s.publishLookahead(ctx, key, choices, planned)
}

func (s *Service) publishLookahead(ctx context.Context, key lookaheadKey, choices, planned []playbackChoice) bool {
	_, _, latest, err := ReadConfig(s.request.ConfigPath)
	if s.ctx.Err() != nil || errors.Is(ctx.Err(), context.Canceled) || err != nil || !s.actionMu.TryLock() {
		return false
	}
	defer s.actionMu.Unlock()
	if s.currentLookaheadKey(latest) != key {
		return false
	}
	state := &s.lookahead
	state.planned = slices.Clone(planned)
	if !slices.EqualFunc(state.choices, choices, sameLookaheadChoice) {
		if !slices.EqualFunc(state.choices[:min(1, len(state.choices))], choices[:min(1, len(choices))], sameLookaheadChoice) {
			state.revision++
		}
		state.choices = slices.Clone(choices)
		s.publishStatus()
	}
	return true
}

func (s *Service) prepareNext(ctx context.Context, config Config, history []Entry, nextIndex int, selection selectionContext, current playbackChoice, planned *[]playbackChoice) playbackChoice {
	var choice playbackChoice
	for i := nextIndex; i < len(history) && ctx.Err() == nil; i++ {
		entry := history[i]
		attempt, cancel := context.WithTimeout(ctx, 4*time.Second)
		ready, err := s.materializeSelection(attempt, playbackChoice{identity: entry.Identity, sourceID: entry.SourceID, historyIndex: i}, config, selection)
		cancel()
		if err == nil {
			choice = ready
			break
		}
	}
	for choice.identity.ImageID == 0 && len(*planned) > 0 && ctx.Err() == nil {
		next := (*planned)[0]
		*planned = (*planned)[1:]
		attempt, cancel := context.WithTimeout(ctx, 4*time.Second)
		ready, err := s.materializeSelection(attempt, next, config, selection)
		cancel()
		if err == nil && ready.hash != current.hash && !queuedCandidate(catalog.Candidate{Identity: ready.identity, Hash: ready.hash}, selection.queued) {
			choice = ready
		}
	}
	if choice.identity.ImageID == 0 {
		choice, _ = s.selectAlternative(ctx, config, func(string) {}, selection, current, playbackChoice{}, false)
	}
	if choice.identity.ImageID > 0 {
		preview, err := s.Preview(ctx, choice.identity.ImageID)
		if err == nil && preview.Available {
			return choice
		}
	}
	return playbackChoice{}
}

func queuedCandidate(candidate catalog.Candidate, choices []playbackChoice) bool {
	return slices.ContainsFunc(choices, func(choice playbackChoice) bool {
		return candidate.Identity.ImageID == choice.identity.ImageID || candidate.Hash != "" && candidate.Hash == choice.hash
	})
}

func (s *Service) planLookahead(ctx context.Context, config Config, selection selectionContext, current playbackChoice, choices, planned []playbackChoice) []playbackChoice {
	if len(choices)+len(planned) >= lookaheadLimit {
		return planned
	}
	pools, err := s.selectionPools(ctx, config, maxCatalogCandidates, func(string) {}, selection)
	if err != nil {
		return planned
	}
	draw := selection.draw
	if draw == nil {
		draw = rand.IntN
	}
	pools = filterPools(pools, func(candidate catalog.Candidate) bool {
		return !queuedCandidate(candidate, []playbackChoice{current}) &&
			!queuedCandidate(candidate, choices) && !queuedCandidate(candidate, planned)
	})
	counts := candidateCounts(pools)
	for len(choices)+len(planned) < lookaheadLimit && ctx.Err() == nil {
		next, _ := freshDraws(config.Playback.RotationMode, pools, nil, draw)
		candidate, ok := next()
		if !ok {
			break
		}
		identity := candidate.Identity
		choice := playbackChoice{identity: identity, hash: candidate.Hash, sourceID: identity.InputID, historyIndex: -1, candidates: counts[discoveryKey(identity.ProviderID, identity.InputID)]}
		planned = append(planned, choice)
		pools = filterPools(pools, func(candidate catalog.Candidate) bool {
			return !queuedCandidate(candidate, []playbackChoice{choice})
		})
	}
	return planned
}

// Stop preparation before a navigation can replace the accepted or pending
// path that the worker protected. Callers hold actionMu. The worker only
// tries to reacquire it and always closes done before starting another pass.
func (s *Service) stopLookaheadWork() {
	if s.lookahead.cancel != nil {
		s.lookahead.cancel()
		<-s.lookahead.done
		s.lookahead.cancel, s.lookahead.done = nil, nil
	}
}

// preparedChoiceAt is advisory. Playback tries earlier forward history first
// and revalidates the queued entry before handing its path to the desktop.
func (s *Service) preparedChoiceAt(saved []byte, index int) playbackChoice {
	if len(s.lookahead.choices) > index && s.lookahead.key == s.currentLookaheadKey(saved) {
		return s.lookahead.choices[index]
	}
	return playbackChoice{}
}

func sameLookaheadChoice(a, b playbackChoice) bool {
	return a.identity == b.identity && a.hash == b.hash && a.historyIndex == b.historyIndex
}

func (s *Service) finishLookahead(consumed int) {
	state := &s.lookahead
	state.revision++
	if consumed > 0 {
		// The history window only shifts when a fresh insertion prunes the
		// oldest entry. A retained forward entry cannot follow that insertion.
		state.choices = slices.Delete(state.choices, 0, consumed)
		state.key.cursorID = s.state.History[s.state.Cursor].ID
	} else {
		state.choices, state.planned = nil, nil
	}
}
