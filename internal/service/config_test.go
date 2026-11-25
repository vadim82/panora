package service

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/vadim82/panora/internal/playback"
)

func TestDefaultConfigPublicationIsPrivateAtomicAndIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panora", "config.toml")
	var created atomic.Int32
	var group sync.WaitGroup
	for range 12 {
		group.Go(func() {
			fresh, err := playback.InitializeDefaultConfig(path, false)
			if err != nil {
				t.Errorf("create: %v", err)
			}
			if fresh {
				created.Add(1)
			}
			config, _, err := playback.LoadConfig(path)
			if err != nil || config.HasEnabledInputs() || config.Filters.MinWidth != 1 || config.Filters.MinHeight != 1 || config.Filters.Ratio != "any" {
				t.Errorf("published incomplete defaults: %+v %v", config, err)
			}
		})
	}
	group.Wait()
	if created.Load() != 1 {
		t.Fatalf("created %d times", created.Load())
	}
	for _, item := range []struct {
		path string
		mode os.FileMode
	}{{path, 0600}, {filepath.Dir(path), 0700}} {
		info, err := os.Stat(item.path)
		if err != nil || info.Mode().Perm() != item.mode {
			t.Fatalf("mode %s: %v %v", item.path, info, err)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 || entries[0].Name() != "config.toml" {
		t.Fatalf("temporary config leaked: %v %v", entries, err)
	}
	for _, data := range [][]byte{[]byte("version = 99\n"), []byte("corrupt = [\n")} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if fresh, err := playback.InitializeDefaultConfig(path, true); err != nil || fresh {
			t.Fatalf("existing config replaced: %v %v", fresh, err)
		}
		got, _ := os.ReadFile(path)
		if !bytes.Equal(got, data) {
			t.Fatal("existing config was reset")
		}
		if _, _, err := playback.LoadConfig(path); err == nil {
			t.Fatal("corrupt/unsupported settings accepted")
		}
	}
}

func TestExplicitMissingConfigIsNotBootstrapped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.toml")
	got, created, err := PrepareConfig(path, io.Discard, true)
	if err != nil || got != path || created {
		t.Fatalf("resolve explicit config: %q %v", got, err)
	}
	if _, _, err := playback.LoadConfig(got); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing explicit config did not fail: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("explicit typo created a settings file")
	}
}

func TestDefaultConfigFailureDoesNotPublishPartialFile(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parent, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := playback.InitializeDefaultConfig(filepath.Join(parent, "config.toml"), false); err == nil {
		t.Fatal("unusable config directory accepted")
	}
	got, _ := os.ReadFile(parent)
	if string(got) != "keep" {
		t.Fatal("failed initialization changed existing data")
	}
}
