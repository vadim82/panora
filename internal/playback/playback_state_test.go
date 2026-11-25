package playback

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/source"
)

func TestPlaybackStateValidationAndRetention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	state, err := LoadState(path)
	if err != nil || state.CurrentPath() != "" {
		t.Fatalf("missing state: %+v %v", state, err)
	}
	image := filepath.Join(t.TempDir(), "image.png")
	for range HistoryLimit + 5 {
		state.Record(image, "home")
	}
	if err := saveFixtureState(t, &state, path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadState(path)
	if err != nil || len(loaded.History) != HistoryLimit || loaded.Cursor != HistoryLimit-1 || loaded.CurrentPath() != image {
		t.Fatalf("unexpected roundtrip: %+v %v", loaded, err)
	}
	for _, body := range []string{`null`, `{}`, `{"version":2}`, `{"version":1} {}`, `{"version":1,"history":[{"path":"relative"}]}`, `broken`} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadState(path); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	if _, err := LoadState(filepath.Join(t.TempDir(), "state.json")); err == nil {
		t.Fatal("accepted legacy JSON path")
	}
}

func TestPlaybackLockReleasedAndContended(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	unlock, err := LockState(path)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if release, err := LockState(path); err == nil {
		release()
		t.Fatal("accepted concurrent request")
	}
	unlock()
	release, err := LockState(path)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

// Fixtures create real catalog memberships rather than fabricating foreign keys.
func saveFixtureState(t *testing.T, state *State, path string) error {
	t.Helper()
	store, err := catalog.Open(context.Background(), path)
	if err != nil {
		return err
	}
	defer store.Close()
	for i := range state.History {
		entry := &state.History[i]
		if entry.Identity.ImageID != 0 {
			continue
		}
		ref, _ := json.Marshal(map[string]string{"path": entry.Path})
		candidate := source.Candidate{ID: filepath.Base(entry.Path), MediaKind: "image", Ref: ref, Content: &source.Content{MediaKind: "image", Kind: "local", Path: entry.Path}}
		entry.Identity, err = store.EnsureImage(context.Background(), "local", entry.SourceID, "fixture", entry.SourceID, candidate)
		if err != nil {
			return err
		}
	}
	if state.LastActionAt.IsZero() && len(state.History) > 0 {
		state.LastActionAt = state.History[len(state.History)-1].AcceptedAt
	}
	return store.SaveState(context.Background(), state.catalogState())
}
