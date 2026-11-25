package playback

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/filelock"
)

const HistoryLimit = 100

type Entry struct {
	ID         string           `json:"id"`
	ImageID    int64            `json:"image_id"`
	Identity   catalog.Identity `json:"-"`
	Path       string           `json:"path"`
	SourceID   string           `json:"source_id"`
	AcceptedAt time.Time        `json:"accepted_at"`
}

type playbackIntent struct {
	Identity     catalog.Identity
	HistoryIndex int
	Path         string
	StartedAt    time.Time
}

// State retains accepted requests and recovery state; catalog owns its schema.
type State struct {
	History      []Entry
	Cursor       int
	Paused       bool
	LastActionAt time.Time
	Pending      *playbackIntent
}

func DefaultStatePath() (string, error) {
	if runtime.GOOS == "darwin" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return defaultDatabasePath(filepath.Join(dir, "panora"))
	}
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "state")
	}
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("XDG_STATE_HOME must be absolute")
	}
	return defaultDatabasePath(filepath.Join(dir, "panora"))
}

func defaultDatabasePath(dir string) (string, error) {
	legacy := filepath.Join(dir, "state.json")
	if _, err := os.Stat(legacy); err == nil {
		return "", fmt.Errorf("legacy state.json is unsupported; select a fresh SQLite path with --state-file")
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("check legacy state: %w", err)
	}
	return filepath.Join(dir, "state.db"), nil
}

func LoadState(path string) (State, error) {
	store, err := catalog.Open(context.Background(), path)
	if err != nil {
		return State{}, err
	}
	defer store.Close()
	return loadState(context.Background(), store)
}

func loadState(ctx context.Context, store *catalog.Store) (State, error) {
	saved, err := store.LoadState(ctx)
	if err != nil {
		return State{}, err
	}
	state := State{Cursor: saved.Cursor, Paused: saved.Paused, LastActionAt: saved.LastActionAt}
	for _, entry := range saved.History {
		state.History = append(state.History, Entry{ID: entry.Reference, ImageID: entry.Identity.ImageID, Path: entry.Path, SourceID: entry.Identity.InputID, AcceptedAt: entry.AcceptedAt, Identity: entry.Identity})
	}
	if saved.Pending != nil {
		p := saved.Pending
		state.Pending = &playbackIntent{Path: p.Path, StartedAt: p.StartedAt, Identity: p.Identity, HistoryIndex: p.HistoryIndex}
	}
	if err := state.validate(); err != nil {
		return State{}, err
	}
	return state, nil
}

func (state State) catalogState() catalog.State {
	saved := catalog.State{Cursor: state.Cursor, Paused: state.Paused, LastActionAt: state.LastActionAt}
	for _, e := range state.History {
		saved.History = append(saved.History, catalog.Record{Reference: e.ID, Path: e.Path, AcceptedAt: e.AcceptedAt, Identity: e.Identity})
	}
	if state.Pending != nil {
		p := state.Pending
		saved.Pending = &catalog.Intent{Path: p.Path, StartedAt: p.StartedAt, Identity: p.Identity, HistoryIndex: p.HistoryIndex}
	}
	return saved
}

func (state State) validate() error {
	if len(state.History) > HistoryLimit {
		return fmt.Errorf("playback history exceeds %d entries", HistoryLimit)
	}
	if (len(state.History) == 0 && state.Cursor != -1) || (len(state.History) > 0 && (state.Cursor < 0 || state.Cursor >= len(state.History) || state.LastActionAt.IsZero())) {
		return fmt.Errorf("invalid history cursor or timestamp")
	}

	for _, entry := range state.History {
		if !filepath.IsAbs(entry.Path) || entry.SourceID == "" || entry.AcceptedAt.IsZero() {
			return fmt.Errorf("invalid playback history entry")
		}
	}
	if state.Pending != nil && (!filepath.IsAbs(state.Pending.Path) || state.Pending.Identity.InputID == "" || state.Pending.StartedAt.IsZero()) {
		return fmt.Errorf("invalid pending playback intent")
	}
	return nil
}

func (state State) CurrentPath() string {
	index := state.Cursor
	if index < 0 || index >= len(state.History) {
		return ""
	}
	return state.History[index].Path
}

func (state *State) moveCursor(index int) {
	state.Cursor = index
	state.LastActionAt = time.Now().UTC()
}

func (state *State) Record(path, sourceID string) {
	state.History = append(state.History, Entry{ID: rand.Text(), Path: path, SourceID: sourceID, AcceptedAt: time.Now().UTC()})
	if len(state.History) > HistoryLimit {
		state.History = state.History[len(state.History)-HistoryLimit:]
	}
	state.moveCursor(len(state.History) - 1)
}

// LockState grants exclusive playback state ownership until release.
func LockState(path string) (func(), error) {
	release, err := filelock.Try(path + ".lock")
	if err != nil {
		return nil, fmt.Errorf("lock playback state (another service may be running): %w", err)
	}
	return release, nil
}
