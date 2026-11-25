package playback

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/vadim82/panora/internal/cache"
	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/desktop"
)

// actionMu gives the scheduler and clients one owner for mutations. Status has
// its own short-lived lock so it stays readable during scanning or rendering.
type Service struct {
	instanceID             string
	draw                   func(int) int
	cache                  *cache.Cache
	cacheApplyError        *Error
	store                  *catalog.Store
	ctx                    context.Context
	request                Request
	resolve                func(context.Context) (desktop.WallpaperBackend, error)
	backend                string
	interval               time.Duration
	actionMu               sync.Mutex
	navigation             navigationState
	configMu               sync.RWMutex
	state                  State
	exclusions             catalog.ExclusionSet
	historyPreviewRevision uint64
	currentHash            string
	currentImage           *CurrentImage
	nextAt                 time.Time
	busy                   bool
	lastError              *Error
	warnings               []string
	statusMu               sync.RWMutex
	status                 Status
	desktopWarning         string // Guarded by statusMu; observer diagnostics only.
	wake                   chan struct{}
	fatal                  chan error
	cancel                 context.CancelFunc
	release                func()
	closeOnce              sync.Once
	discoveryMu            sync.Mutex
	discoveryDone          chan struct{}
	discoveryStarted       bool
	effectiveSettings      EffectiveSettings // Guarded by statusMu after startup.
	retryMu                sync.Mutex
	retries                map[catalog.RetryKey]catalog.RetryState
	discoveryWake          chan struct{}
	theme                  themeRuntime
	lookahead              lookaheadState // Guarded by actionMu.
	lookaheadWake          chan struct{}
	lookaheadDone          chan struct{} // Guarded by discoveryMu.
	pendingImageID         int64
}

func newService(ctx context.Context, options ServiceOptions, resolve Resolver, config Config) (*Service, error) {
	request := options.Request
	store, err := catalog.Open(ctx, request.StatePath)
	if err != nil {
		return nil, err
	}
	opened := false
	defer func() {
		if !opened {
			store.Close()
		}
	}()
	if err := store.UpgradeInputRevisions(ctx, legacyInputRevisions(config)); err != nil {
		return nil, err
	}
	state, err := loadState(ctx, store)
	if err != nil {
		return nil, err
	}
	exclusions, err := store.LoadExclusions(ctx)
	if err != nil {
		return nil, err
	}
	var currentHash string
	var currentImage *CurrentImage
	if state.Cursor >= 0 {
		entry := state.History[state.Cursor]
		currentHash, err = store.ContentHash(ctx, entry.Identity.ImageID)
		if err != nil {
			return nil, err
		}
		currentImage = loadCurrentImage(ctx, store, entry)
	}
	loadedRetries, err := store.LoadRetries(ctx)
	if err != nil {
		return nil, err
	}
	retries := map[catalog.RetryKey]catalog.RetryState{}
	for _, retry := range loadedRetries {
		retries[retry.RetryKey] = retry
	}
	if request.CachePath == "" {
		request.CachePath, err = DefaultCachePath()
		if err != nil {
			return nil, err
		}
	}
	imageCache, err := cache.New(cache.Options{Dir: request.CachePath, MaxImages: config.Cache.MaxImages, MaxBytes: config.Cache.MaxBytes})
	if err != nil {
		return nil, err
	}
	state.Paused = state.Paused || options.Paused || state.Pending != nil
	if err := store.SaveState(ctx, state.catalogState()); err != nil {
		imageCache.Close()
		return nil, err
	}
	serviceCtx, cancel := context.WithCancel(ctx)
	s := &Service{
		instanceID:    rand.Text(),
		status:        Status{SetupRequired: !config.HasEnabledInputs()},
		exclusions:    exclusions,
		currentHash:   currentHash,
		currentImage:  currentImage,
		retries:       retries,
		discoveryWake: make(chan struct{}, 1),
		discoveryDone: make(chan struct{}),
		lookaheadWake: make(chan struct{}, 1),
		cache:         imageCache,
		store:         store,
		ctx:           serviceCtx,
		cancel:        cancel,
		request:       request,
		resolve:       resolve,
		backend:       options.Backend,
		interval:      options.Interval,
		state:         state,
		wake:          make(chan struct{}, 1),
		fatal:         make(chan error, 1),
		theme: themeRuntime{
			home:         defaultThemeHome(),
			done:         make(chan struct{}),
			freshProfile: options.FreshOmarchyProfile && state.LastActionAt.IsZero(),
		},
	}
	if state.Pending != nil {
		s.lastError = &Error{Code: "apply_uncertain", Message: "an interrupted application may have reached the desktop; inspect it and use next or previous before resuming"}
	}
	s.effectiveSettings = EffectiveSettings{Backend: options.Backend, Fit: request.Options.Fit, Upscale: request.Options.Upscale, Interval: options.Interval.String(), CacheMaxImages: config.Cache.MaxImages, CacheMaxBytes: config.Cache.MaxBytes}
	s.applyCachePreferences(config, UpdateConfigurationRequest{CacheMaxImages: &config.Cache.MaxImages, CacheMaxBytes: &config.Cache.MaxBytes})
	s.resetDeadline()
	s.publishStatus()
	opened = true
	return s, nil
}

func (s *Service) Status() Status {
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()
	status := s.status
	theme := s.theme.observation.Load()
	if status.PreparedNextImageID > 0 {
		var themeID string
		if theme != nil && theme.enabled {
			themeID = theme.active.ID
		}
		if status.preparedNextTheme != themeID {
			status.PreparedNextImageID, status.PreparedNextConfigRevision, status.PreparedNextRevision = 0, "", 0
			status.PreparedNextPreviewContent = ""
			status.PreparedBeyond = nil
		}
	}
	if theme != nil {
		status.OmarchyThemeName = theme.active.Label()
		status.OmarchyThemeRevision = theme.active.ID
		if collection := s.theme.collection.Load(); collection != nil && collection.active == theme.active {
			status.OmarchyThemeIssue = collection.warning
		}
	}
	if status.SetupRequired {
		status.NextAt = nil
	}
	status.Warnings = append([]string(nil), status.Warnings...)
	if s.desktopWarning != "" {
		status.Warnings = append(status.Warnings, s.desktopWarning)
	}
	status.RefreshWarnings = append([]string(nil), status.RefreshWarnings...)
	status.Providers = cloneProviderHealth(status.Providers)
	if status.Current != nil {
		entry := *status.Current
		status.Current = &entry
	}
	if status.CurrentImage != nil {
		status.CurrentImage = status.CurrentImage.clone()
	}
	for _, entry := range []**HistoryReference{
		&status.PreviousHistory, &status.ForwardHistory, &status.PreviousBeyond, &status.ForwardBeyond, &status.PreparedBeyond,
	} {
		if *entry != nil {
			copy := **entry
			*entry = &copy
		}
	}
	if status.NextAt != nil {
		next := *status.NextAt
		if theme != nil && theme.enabled && !theme.settled.IsZero() {
			interval, _ := time.ParseDuration(status.Interval)
			next = maxTime(next, themeReadyAt(theme, interval, theme.onlySource && s.firstThemeApply(status.HistoryLength)))
		}
		status.NextAt = &next
	}
	if status.LastError != nil {
		lastError := *status.LastError
		status.LastError = &lastError
	}
	return status
}

// Called while actionMu is held (or before the service starts).
func (s *Service) publishStatus() {
	status := Status{ExclusionUndoAvailable: s.exclusions.Count > 0, Version: StatusVersion, Paused: s.state.Paused, Busy: s.busy, Uncertain: s.state.Pending != nil, Interval: s.interval.String(), HistoryLength: len(s.state.History), HistoryCursor: s.state.Cursor, Backend: s.backend, LastError: s.lastError, Warnings: append([]string(nil), s.warnings...)}
	status.InstanceID = s.instanceID
	status.NavigationSupported = true
	status.HistoryPreviewRevision = s.historyPreviewRevision
	status.previewExclusions = s.exclusions
	status.PendingImageID = s.pendingImageID
	if s.pendingImageID > 0 && s.state.Pending != nil {
		status.PendingPreviewContent = previewContentRevision(s.state.Pending.Path)
	}
	if len(s.lookahead.choices) > 0 && s.state.Pending == nil &&
		s.lookahead.key.exclusionRevision == s.historyPreviewRevision && s.lookahead.key.backend == s.backend &&
		s.state.Cursor >= 0 && s.lookahead.key.cursorID == s.state.History[s.state.Cursor].ID {
		status.PreparedNextImageID = s.lookahead.choices[0].identity.ImageID
		status.PreparedNextPreviewContent = s.lookahead.choices[0].hash
		status.PreparedNextConfigRevision = s.lookahead.key.configRevision
		status.PreparedNextRevision = s.lookahead.revision
		status.preparedNextTheme = s.lookahead.key.themeID
		if len(s.lookahead.choices) > 1 {
			choice := s.lookahead.choices[1]
			status.PreparedBeyond = &HistoryReference{ImageID: choice.identity.ImageID, ContentRevision: choice.hash}
		}
	}
	index := s.state.Cursor
	if index >= 0 && index < len(s.state.History) {
		entry := s.state.History[index]
		status.Current = &Entry{ID: entry.ID, ImageID: entry.Identity.ImageID, Path: entry.Path, SourceID: entry.SourceID, AcceptedAt: entry.AcceptedAt}
		status.CurrentImage = s.currentImage
		status.CurrentPreviewContent = previewContentRevision(entry.Path)
		for i, neighbor := range []struct {
			offset int
			ref    **HistoryReference
		}{
			{-1, &status.PreviousHistory},
			{1, &status.ForwardHistory},
			{-2, &status.PreviousBeyond},
			{2, &status.ForwardBeyond},
		} {
			position := index + neighbor.offset
			if position >= 0 && position < len(s.state.History) {
				entry := s.state.History[position]
				*neighbor.ref = &HistoryReference{ID: entry.ID, ImageID: entry.Identity.ImageID, ContentRevision: previewContentRevision(entry.Path)}
				status.previewNeighbors[i] = neighborPreviewEntry{id: entry.ID, identity: entry.Identity}
			}
		}
		status.CurrentExcluded = s.exclusions.Contains(entry.Identity.ImageID, s.currentHash)
	}
	if !s.state.Paused && !s.nextAt.IsZero() {
		next := s.nextAt
		status.NextAt = &next
	}
	s.statusMu.Lock()
	status.SetupRequired = s.status.SetupRequired
	status.RefreshHealthVersion = RefreshHealthVersion
	status.Providers = s.status.Providers
	status.Refreshing = s.status.Refreshing
	status.RefreshWarnings = s.status.RefreshWarnings
	s.status = status
	s.statusMu.Unlock()
}

// Configuration discovery can change setup state without taking playback ownership.
func (s *Service) setSetupRequired(required bool) {
	s.statusMu.Lock()
	changed := s.status.SetupRequired != required
	s.status.SetupRequired = required
	s.statusMu.Unlock()
	if changed {
		s.notify()
	}
}

func (s *Service) resetDeadline() {
	s.nextAt = s.state.LastActionAt.Add(s.interval)
	if s.state.LastActionAt.IsZero() {
		s.nextAt = time.Now()
	}
}

func (s *Service) notify() {
	s.wakeLookahead()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) Run(reportFailure func(error)) {
	for s.ctx.Err() == nil {
		status := s.Status()
		if status.Paused || status.SetupRequired {
			select {
			case <-s.ctx.Done():
				return
			case <-s.wake:
				continue
			}
		}
		if status.NextAt != nil && time.Until(*status.NextAt) > 0 {
			s.wait(time.Until(*status.NextAt))
			continue
		}
		_, err := s.perform(s.ctx, "next", actionOptions{automatic: true})
		if err != nil {
			var apiErr *Error
			if errors.As(err, &apiErr) && apiErr.Code == "busy" {
				interval, _ := time.ParseDuration(status.Interval)
				s.wait(min(interval, time.Second))
			} else if s.ctx.Err() == nil && reportFailure != nil {
				reportFailure(err)
			}
		}
	}
}

func (s *Service) wait(delay time.Duration) {
	timer := time.NewTimer(max(delay, time.Millisecond))
	defer timer.Stop()
	select {
	case <-s.ctx.Done():
	case <-s.wake:
	case <-timer.C:
	}
}

func (s *Service) Action(ctx context.Context, action string) (Status, error) {
	return s.perform(ctx, action, actionOptions{})
}

type actionOptions struct {
	automatic   bool
	exclusionID int64
	historyID   string
	exclude     *currentExclusion
	navigation  *NavigationRequest
}

func (s *Service) perform(ctx context.Context, action string, options actionOptions) (Status, error) {
	if !s.actionMu.TryLock() {
		return s.Status(), &Error{Code: "busy", Message: "another playback operation is running; try again after it finishes"}
	}
	defer s.actionMu.Unlock()
	if s.ctx.Err() != nil || ctx.Err() != nil {
		return s.Status(), &Error{Code: "canceled", Message: "playback request canceled or service stopping"}
	}
	if options.automatic && (s.state.Paused || s.Status().SetupRequired || time.Now().Before(s.nextAt)) {
		return s.Status(), nil
	}
	if options.exclude != nil {
		if err := s.checkExclusionTarget(*options.exclude); err != nil {
			return s.Status(), err
		}
	}
	opCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	defer cancel()
	preparation := opCtx
	var navigation *navigationWork
	if options.navigation != nil {
		var failure *Error
		preparation, navigation, failure = s.beginNavigation(opCtx, *options.navigation)
		if failure != nil {
			return s.Status(), failure
		}
	}
	if action == "next" || action == "previous" || action == "replay" {
		s.stopLookaheadWork()
		var failure *Error
		preparation, failure = s.beginThemeOperation(preparation, options.automatic)
		if failure != nil {
			if navigation != nil && s.finishNavigation(navigation) {
				failure = supersededNavigation()
			}
			return s.Status(), failure
		}
	}
	s.busy = true
	s.publishStatus()
	var err *Error
	previousWarnings := s.warnings
	switch action {
	case "pause", "resume":
		err = s.setPaused(action == "pause")
	case "next", "previous":
		offset := 1
		if action == "previous" {
			offset = -1
		}
		if options.navigation != nil {
			offset = options.navigation.Offset
		}
		err = s.changeImage(opCtx, preparation, offset, navigation)
	case "replay":
		err = s.replayHistory(preparation, options.historyID)
		s.finishLookahead(false, false)
	case "exclude", "undo-exclude":
		err = s.changeExclusion(opCtx, action, 0)
	case "restore-exclusion":
		if options.exclusionID <= 0 {
			err = &Error{Code: "invalid_request", Message: "exclusion ID must be positive"}
		} else {
			err = s.changeExclusion(opCtx, action, options.exclusionID)
		}
	default:
		err = &Error{Code: "invalid_action", Message: "unknown playback action"}
	}
	if navigation != nil && s.finishNavigation(navigation) {
		s.warnings = previousWarnings
		err = supersededNavigation()
	}
	if err != nil && err.Code != "navigation_superseded" {
		s.lastError = err
		if action == "next" || action == "previous" {
			if err.Code == "discovery_pending" || err.Code == "theme_changed" {
				s.nextAt = time.Now().Add(time.Second)
			} else {
				s.nextAt = time.Now().Add(s.interval)
			}
		}
	}
	s.busy = false
	s.publishStatus()
	s.notify()
	if err != nil {
		return s.Status(), err
	}
	return s.Status(), nil
}

func (s *Service) setPaused(paused bool) *Error {
	if !paused && s.state.Pending != nil {
		return &Error{Code: "apply_uncertain", Message: "inspect the desktop and use next or previous to resolve the uncertain application before resuming"}
	}
	if s.state.Paused == paused {
		return nil
	}
	next := s.state
	next.Paused = paused
	if !paused {
		next.LastActionAt = time.Now().UTC()
	}
	if err := s.saveState(next); err != nil {
		return &Error{Code: "state_save_failed", Message: err.Error()}
	}
	s.state = next
	s.resetDeadline()
	return nil
}

func (s *Service) saveState(state State) error {
	return s.store.SaveState(context.Background(), state.catalogState())
}

type playbackChoice struct {
	hash           string
	identity       catalog.Identity
	path, sourceID string
	historyIndex   int // -1 denotes a fresh random selection.
	image          *CurrentImage
}

func (s *Service) chooseImage(ctx context.Context, offset int) (playbackChoice, bool, *Error) {
	s.warnings = nil
	config, _, saved, err := ReadConfig(s.request.ConfigPath)
	if err != nil {
		return playbackChoice{}, false, invalidSavedConfiguration()
	}
	s.setSetupRequired(!config.HasEnabledInputs())
	if !config.HasEnabledInputs() {
		return playbackChoice{}, false, &Error{Code: "setup_required", Message: "add or enable a wallpaper source to begin playback"}
	}
	if resolution := s.availableSourceRules(ctx, config); resolution.State != "resolved" {
		return playbackChoice{}, false, unresolvedRuleError(resolution)
	}
	var prepared playbackChoice
	if offset > 0 {
		prepared = s.preparedChoiceAt(saved, offset-1)
	}
	warn := func(message string) {
		if len(s.warnings) < 20 {
			s.warnings = append(s.warnings, message)
		}
	}
	step := 1
	if offset < 0 {
		step = -1
	}
	for index := s.state.Cursor + offset; index >= 0 && index < len(s.state.History); index += step {
		if err := ctx.Err(); err != nil {
			return playbackChoice{}, false, &Error{Code: "canceled", Message: err.Error()}
		}
		entry := s.state.History[index]
		choice, err := s.materializeChoice(ctx, playbackChoice{path: entry.Path, sourceID: entry.SourceID, historyIndex: index, identity: entry.Identity}, config)
		if err != nil {
			warn(fmt.Sprintf("Skipping %s: %v", entry.Path, err))
			continue
		}
		return choice, offset == 1 && prepared.historyIndex == index && prepared.identity == choice.identity && prepared.hash == choice.hash, nil
	}
	if offset < 0 {
		return playbackChoice{}, false, &Error{Code: "no_previous", Message: "no earlier available image matches the current folders and filters"}
	}
	if prepared.historyIndex == -1 && prepared.identity.ImageID > 0 {
		choice, err := s.materializeChoice(ctx, prepared, config)
		if err == nil && choice.hash == prepared.hash {
			return choice, offset == 1, nil
		}
	}
	if offset > 1 {
		first := s.preparedChoiceAt(saved, 0)
		if first.identity.ImageID > 0 {
			var current playbackChoice
			if s.state.Cursor >= 0 {
				current.identity, current.hash = s.state.History[s.state.Cursor].Identity, s.currentHash
			}
			choice, selectionErr := s.selectAlternative(ctx, config, warn, s.selectionContext(), current, first, true)
			return choice, false, selectionErr
		}
	}
	choice, selectionErr := s.selectFresh(ctx, config, warn)
	return choice, false, selectionErr
}

func (s *Service) changeImage(ctx, preparation context.Context, offset int, navigation *navigationWork) *Error {
	choice, prepared, selectionErr := s.chooseImage(preparation, offset)
	if selectionErr == nil {
		selectionErr = s.applyPreparedChoice(ctx, preparation, choice, navigation)
	}
	if navigation == nil || preparation.Err() == nil {
		s.finishLookahead(prepared, selectionErr == nil)
	}
	return selectionErr
}

// Every navigation/replay uses the same persisted intent and accepted-state path.
func (s *Service) applyChoice(ctx context.Context, choice playbackChoice) *Error {
	return s.applyPreparedChoice(ctx, ctx, choice, nil)
}

func (s *Service) applyPreparedChoice(ctx, preparation context.Context, choice playbackChoice, navigation *navigationWork) *Error {
	_, guard, failure := s.guardThemeChoice(preparation, choice)
	if failure != nil {
		return failure
	}
	backend, err := s.resolveDesktop(preparation)
	if err != nil {
		return &Error{Code: "backend_unavailable", Message: err.Error()}
	}
	if guard != nil {
		if !guard() {
			return staleThemeError()
		}
		ctx = desktop.WithOmarchyDispatchGuard(ctx, guard)
	}
	return s.applyWithPreparation(ctx, preparation, backend, choice, false, navigation)
}

func (s *Service) resolveDesktop(ctx context.Context) (desktop.WallpaperBackend, error) {
	if s.request.ResolveBackend != nil {
		return s.request.ResolveBackend(ctx, s.backend)
	}
	return s.resolve(ctx)
}

// Reapplication shares persisted intent/failure handling with navigation. It
// leaves history, acceptance time and the rotation deadline unchanged on success.
func (s *Service) applyDesktop(ctx context.Context, backend desktop.WallpaperBackend, choice playbackChoice, reapply bool) *Error {
	return s.applyWithPreparation(ctx, ctx, backend, choice, reapply, nil)
}

func (s *Service) applyWithPreparation(ctx, preparation context.Context, backend desktop.WallpaperBackend, choice playbackChoice, reapply bool, navigation *navigationWork) *Error {
	prior := s.state
	started := false
	begin := func() error {
		if navigation != nil {
			if err := s.beginNavigationDispatch(ctx, navigation); err != nil {
				return err
			}
		}
		intent := prior
		intent.Pending = &playbackIntent{Path: choice.path, StartedAt: time.Now().UTC(), Identity: choice.identity, HistoryIndex: choice.historyIndex}
		if err := s.saveState(intent); err != nil {
			return &Error{Code: "state_save_failed", Message: err.Error()}
		}
		s.state = intent
		if !reapply {
			s.pendingImageID = choice.identity.ImageID
		}
		s.publishStatus()
		started = true
		return nil
	}
	defer func() { s.pendingImageID = 0 }()
	options := s.request.Options
	native, isOmarchy := backend.(desktop.OmarchyBackend)
	if isOmarchy && choice.identity.ProviderID == omarchyProvider &&
		(options.Fit == "" || options.Fit == "auto") && !options.Upscale {
		// Match the fill Omarchy uses when it opens a native theme wallpaper.
		options.Fit = "crop"
	}
	var err error
	if isOmarchy && navigation != nil {
		err = native.SetWallpaperWithPreparation(ctx, preparation, choice.path, options, begin)
	} else if err = begin(); err == nil {
		err = backend.SetWallpaper(ctx, choice.path, options)
	}
	if err != nil {
		if !started {
			var failure *Error
			if errors.As(err, &failure) {
				return failure
			}
			if errors.Is(err, desktop.ErrOmarchyThemeChanged) {
				return staleThemeError()
			}
			return &Error{Code: "apply_failed", Message: fmt.Sprintf("could not prepare wallpaper for dispatch: %v", err)}
		}
		if errors.Is(err, desktop.ErrOmarchyThemeChanged) {
			// No dispatch occurred. Restore any uncertainty left by an earlier
			// request, since this attempt did not resolve it.
			if saveErr := s.saveState(prior); saveErr != nil {
				s.stopAfterSaveFailure(saveErr)
				return &Error{Code: "state_save_failed", Message: saveErr.Error()}
			}
			s.state = prior
			return staleThemeError()
		}
		// Even an error or timeout can follow a desktop change. Preserve the intent
		// and require an explicit action, rather than replaying it automatically.
		s.state.Paused = true
		if saveErr := s.saveState(s.state); saveErr != nil {
			s.stopAfterSaveFailure(saveErr)
		}
		return &Error{Code: "apply_uncertain", Message: fmt.Sprintf("wallpaper application failed and may have reached the desktop: %v; rotation is paused; inspect status and use next or previous", err)}
	}
	next := s.state
	next.Pending = nil
	if reapply {
		// The current accepted entry stays exactly as it was.
	} else if choice.historyIndex >= 0 {
		next.History = append([]Entry(nil), next.History...)
		next.History[choice.historyIndex].Path = choice.path
		next.moveCursor(choice.historyIndex)
	} else {
		next.Record(choice.path, choice.sourceID)
		next.History[len(next.History)-1].Identity = choice.identity
		next.History[len(next.History)-1].ImageID = choice.identity.ImageID
	}
	if err := s.saveState(next); err != nil {
		s.state.Paused = true
		s.stopAfterSaveFailure(err)
		return &Error{Code: "state_save_after_apply", Message: fmt.Sprintf("wallpaper request accepted but state could not be saved: %v; stopping service; inspect status after restart", err)}
	}
	s.state = next
	s.currentHash = choice.hash
	if !reapply {
		s.currentImage = choice.image
		s.lastError = nil
		s.resetDeadline()
	}
	return nil
}

func (s *Service) stopAfterSaveFailure(err error) {
	select {
	case s.fatal <- fmt.Errorf("playback state could not be saved; service stopped to avoid unrecorded applications: %w", err):
	default:
	}
}
