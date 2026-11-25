package playback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/vadim82/panora/internal/cache"
	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/omarchytheme"
	"github.com/vadim82/panora/internal/source"
)

type themeStill struct {
	file      omarchytheme.Still
	thumb     cache.Preview
	hash      string
	candidate catalog.Candidate
}

type themeCollection struct {
	active   omarchytheme.Active
	palette  omarchytheme.Palette
	sourceID string
	stills   []themeStill
	warning  string
}

type themeObservation struct {
	active     omarchytheme.Active
	settled    time.Time
	enabled    bool
	onlySource bool
}

type themeDiscoveryResult struct {
	collection *themeCollection
	files      []omarchytheme.Still
}

type themeRuntime struct {
	home         string
	done         chan struct{}
	freshProfile bool
	collection   atomic.Pointer[themeCollection]
	observation  atomic.Pointer[themeObservation]
}

// Theme discovery owns asynchronous original reads and completed thumbnail
// snapshots. Playback's scheduler only consumes its observations.
func (s *Service) runThemeDiscovery() {
	defer close(s.theme.done)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	workDone := make(chan themeDiscoveryResult, 1)
	var workCancel context.CancelFunc
	var workActive omarchytheme.Active
	var workID string
	defer func() {
		if workCancel != nil {
			workCancel()
			<-workDone
		}
	}()
	var lastPoll time.Time
	var priorFiles []omarchytheme.Still
	for s.ctx.Err() == nil {
		config, _, err := LoadConfig(s.request.ConfigPath)
		sourceConfig := config.OmarchyTheme
		if err != nil || sourceConfig == nil || s.theme.home == "" {
			s.theme.collection.Store(nil)
			s.theme.observation.Store(nil)
			priorFiles = nil
			if workCancel != nil {
				workCancel()
			}
		} else {
			sampleCtx, cancel := context.WithTimeout(s.ctx, time.Second)
			active, ready, readErr := s.sampleTheme(sampleCtx, sourceConfig.Enabled)
			cancel()
			previous := s.theme.observation.Load()
			observed := themeObservation{active: active, enabled: sourceConfig.Enabled, onlySource: !config.hasEnabledProviderInputs()}
			if readErr == nil {
				if previous != nil && previous.active == active && previous.enabled == observed.enabled {
					observed.settled = previous.settled
				}
				if ready && observed.settled.IsZero() {
					observed.settled = time.Now()
				}
				if previous == nil || *previous != observed {
					s.theme.observation.Store(&observed)
					s.notify()
				}
				current := s.theme.collection.Load()
				if current == nil || current.active != active || current.sourceID != sourceConfig.ID {
					if workCancel == nil {
						priorFiles = nil
						lastPoll = time.Time{}
					} else if workActive != active || workID != sourceConfig.ID {
						workCancel()
					}
				}
				if workCancel == nil && time.Since(lastPoll) >= 3*time.Second {
					lastPoll = time.Now()
					current := s.theme.collection.Load()
					files := priorFiles
					ctx, cancel := context.WithTimeout(s.ctx, time.Minute)
					workCancel, workActive, workID = cancel, active, sourceConfig.ID
					go func() {
						workDone <- s.discoverTheme(ctx, active, sourceConfig.ID, current, files)
					}()
				}
			} else if previous != nil {
				// Samples taken during a native transition omit the active key.
				s.theme.observation.Store(nil)
				s.notify()
				if workCancel != nil {
					workCancel()
				}
			}
		}
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		case result := <-workDone:
			workCancel()
			workCancel = nil
			if result.collection == nil {
				continue
			}
			priorFiles = result.files
			current := s.theme.observation.Load()
			if current != nil && current.active == result.collection.active {
				s.theme.collection.Store(result.collection)
				s.notify()
			}
		}
	}
}

func (s *Service) sampleTheme(ctx context.Context, enabled bool) (omarchytheme.Active, bool, error) {
	if runtime.GOOS != "linux" || s.theme.home == "" {
		return omarchytheme.Active{}, false, errors.New("Omarchy theme state is unavailable")
	}
	active, err := omarchytheme.ReadActive(s.theme.home)
	if err != nil {
		return active, false, err
	}
	previous := s.theme.observation.Load()
	if !enabled || previous != nil && previous.active == active && previous.enabled && !previous.settled.IsZero() {
		return active, enabled, nil
	}
	release, err := desktop.TryOmarchyThemeLock()
	if err != nil {
		return active, false, err
	}
	defer release()
	active, err = omarchytheme.ReadActive(s.theme.home)
	if err != nil {
		return active, false, err
	}
	return active, desktop.OmarchyIntroReady(ctx, active.IntroToken), nil
}

func (s *Service) discoverTheme(ctx context.Context, active omarchytheme.Active, id string, previous *themeCollection, before []omarchytheme.Still) themeDiscoveryResult {
	files, err := active.Stills(ctx, s.theme.home)
	collection := &themeCollection{active: active, sourceID: id}
	if err != nil {
		collection.warning = err.Error()
	}
	collection.palette, _ = omarchytheme.ReadPalette(active.Root)
	known := map[string]themeStill{}
	unchanged := map[string]omarchytheme.Still{}
	if previous != nil && previous.active == active && previous.sourceID == id {
		for _, entry := range previous.stills {
			known[entry.file.Path] = entry
		}
		for _, file := range before {
			unchanged[file.Path] = file
		}
	}
	for _, file := range files {
		if ctx.Err() != nil {
			return themeDiscoveryResult{}
		}
		if old, ok := unchanged[file.Path]; ok && old.Same(file) {
			if entry, exists := known[file.Path]; exists {
				collection.stills = append(collection.stills, entry)
			}
			continue
		}
		thumb, digest, err := cache.DiscoverThumbnail(ctx, file.Path)
		if err != nil || !file.Available() {
			continue
		}
		identity := sha256.Sum256([]byte(file.Name + "\x00" + digest))
		item := source.Candidate{ID: hex.EncodeToString(identity[:]), MediaKind: "image", Ref: json.RawMessage(`{}`),
			Dimensions:  &source.Dimensions{Width: thumb.Width, Height: thumb.Height},
			Attribution: &source.Attribution{Title: file.Name},
			Content:     &source.Content{Kind: "local", MediaKind: "image", Path: file.Path}}
		s.configMu.RLock()
		current, configErr := omarchytheme.ReadActive(s.theme.home)
		cfg, _, settingsErr := LoadConfig(s.request.ConfigPath)
		if configErr != nil || current != active || settingsErr != nil || cfg.OmarchyTheme == nil || cfg.OmarchyTheme.ID != id {
			s.configMu.RUnlock()
			return themeDiscoveryResult{}
		}
		catalogID, err := s.store.EnsureImage(ctx, omarchyProvider, id, active.ID, active.ID, item)
		s.configMu.RUnlock()
		if err != nil {
			collection.warning = "Some Omarchy stills could not be cataloged."
			continue
		}
		collection.stills = append(collection.stills, themeStill{file: file, thumb: thumb, hash: digest,
			candidate: catalog.Candidate{Identity: catalogID, Candidate: item, Hash: digest}})
	}
	if len(collection.stills) == 0 && collection.warning == "" {
		collection.warning = "The active theme contains no readable JPEG, PNG, or static WebP stills."
	}
	return themeDiscoveryResult{collection: collection, files: files}
}

func (s *Service) currentThemeCollection() *themeCollection {
	collection := s.theme.collection.Load()
	if collection == nil {
		return nil
	}
	active, err := omarchytheme.ReadActive(s.theme.home)
	if err != nil || active != collection.active {
		return nil
	}
	return collection
}

func (s *Service) themeStillFor(identity catalog.Identity) (themeStill, error) {
	if collection := s.currentThemeCollection(); collection != nil && identity.InputID == collection.sourceID &&
		identity.Revision == collection.active.ID {
		for _, still := range collection.stills {
			if identity.ImageID == still.candidate.Identity.ImageID && still.file.Available() {
				return still, nil
			}
		}
	}
	return themeStill{}, fmt.Errorf("image is outside the active Omarchy theme")
}

func defaultThemeHome() string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS != "linux" || !filepath.IsAbs(home) {
		return ""
	}
	return home
}
