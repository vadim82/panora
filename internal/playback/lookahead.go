package playback

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/vadim82/panora/internal/catalog"
)

const (
	lookaheadSlots   = 2
	lookaheadRefresh = 10 * time.Second
)

type selectionContext struct {
	exclusions  catalog.ExclusionSet
	backend     string
	protected   []string
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
	revision  uint64
	key       lookaheadKey
	choices   []playbackChoice
	refreshAt time.Time
	cancel    context.CancelFunc
	done      chan struct{}
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
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for s.ctx.Err() == nil {
		s.prepareLookahead()
		select {
		case <-s.ctx.Done():
		case <-s.lookaheadWake:
		case <-ticker.C:
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
	if !s.actionMu.TryLock() {
		return
	}
	key := s.currentLookaheadKey(saved)
	slotLimit := min(lookaheadSlots, config.Cache.MaxImages-1)
	state := &s.lookahead
	if key != state.key || len(state.choices) > 0 && (configErr != nil || s.state.Pending != nil) {
		state.key, state.choices, state.refreshAt = key, nil, time.Time{}
		state.revision++
		s.publishStatus()
	}
	if configErr != nil || s.state.Pending != nil || s.state.Cursor < 0 ||
		!config.HasEnabledInputs() || slotLimit < 1 || time.Now().Before(state.refreshAt) {
		s.actionMu.Unlock()
		return
	}
	selection := s.selectionContext()
	selection.speculative = true
	history := s.state.History
	index := s.state.Cursor
	current := playbackChoice{identity: history[index].Identity, hash: s.currentHash}
	choices := slices.Clone(state.choices)
	ctx, cancel := context.WithTimeout(s.ctx, 8*time.Second)
	done := make(chan struct{})
	state.cancel, state.done = cancel, done
	s.actionMu.Unlock()
	defer close(done)
	defer cancel()

	// Recheck retained slots periodically. Preview verification stays behind
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
	// Publish the visible choice before resolving the second. Its preview
	// can be read while the second original is still being staged.
	if len(choices) < slotLimit {
		var extra playbackChoice
		nextIndex := index + 1
		if len(choices) == 0 {
			choice := s.prepareNext(ctx, config, history, nextIndex, selection, current, extra)
			if choice.identity.ImageID > 0 {
				choices = append(choices, choice)
				if slotLimit > 1 && !s.publishLookahead(ctx, key, choices, false) {
					return
				}
			}
		}
		if len(choices) < slotLimit && len(choices) == 1 && ctx.Err() == nil {
			extra = choices[0]
			selection.protected = append(selection.protected, extra.path)
			nextIndex = extra.historyIndex + 1
			if extra.historyIndex < 0 {
				nextIndex = len(history)
			}
			choice := s.prepareNext(ctx, config, history, nextIndex, selection, current, extra)
			if choice.identity.ImageID > 0 {
				choices = append(choices, choice)
			}
		}
	}
	s.publishLookahead(ctx, key, choices, true)
}

func (s *Service) publishLookahead(ctx context.Context, key lookaheadKey, choices []playbackChoice, complete bool) bool {
	_, _, latest, err := ReadConfig(s.request.ConfigPath)
	if s.ctx.Err() != nil || errors.Is(ctx.Err(), context.Canceled) || err != nil || !s.actionMu.TryLock() {
		return false
	}
	defer s.actionMu.Unlock()
	if s.currentLookaheadKey(latest) != key {
		return false
	}
	state := &s.lookahead
	if !slices.EqualFunc(state.choices, choices, sameLookaheadChoice) {
		if !slices.EqualFunc(state.choices[:min(1, len(state.choices))], choices[:min(1, len(choices))], sameLookaheadChoice) {
			state.revision++
		}
		state.choices = slices.Clone(choices)
		s.publishStatus()
	}
	if complete {
		state.refreshAt = time.Now().Add(lookaheadRefresh)
	}
	return true
}

func (s *Service) prepareNext(ctx context.Context, config Config, history []Entry, nextIndex int, selection selectionContext, current, extra playbackChoice) playbackChoice {
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
	if choice.identity.ImageID == 0 {
		choice, _ = s.selectAlternative(ctx, config, func(string) {}, selection, current, extra, false)
	}
	if choice.identity.ImageID > 0 {
		preview, err := s.Preview(ctx, choice.identity.ImageID)
		if err == nil && preview.Available {
			return choice
		}
	}
	return playbackChoice{}
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

func (s *Service) finishLookahead(used, accepted bool) {
	state := &s.lookahead
	state.revision++
	if used && accepted && len(state.choices) == lookaheadSlots {
		remaining := state.choices[1]
		// The history window only shifts when a fresh insertion prunes the
		// oldest entry. A retained forward entry cannot follow that insertion.
		state.choices = []playbackChoice{remaining}
		state.key.cursorID = s.state.History[s.state.Cursor].ID
	} else {
		state.choices = nil
	}
	state.refreshAt = time.Time{}
}
