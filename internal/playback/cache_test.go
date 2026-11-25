package playback

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/vadim82/panora/internal/cache"
	"github.com/vadim82/panora/internal/source"
)

func TestServiceCachesIndependentCopyAndUsesItWithoutOriginal(t *testing.T) {
	service, _ := testService(t, &fakeWallpaperBackend{})
	first := act(t, service, "next")
	original := service.currentOriginPath(context.Background())
	before, err := os.ReadFile(original)
	if err != nil {
		t.Fatal(err)
	}
	cached, err := os.ReadFile(first.Current.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(cached) != string(before) || first.Current.Path == original || filepath.Dir(first.Current.Path) != service.request.CachePath {
		t.Fatal("desktop did not receive independent cached content")
	}
	a, _ := os.Stat(original)
	b, _ := os.Stat(first.Current.Path)
	if os.SameFile(a, b) {
		t.Fatal("cache aliases original inode")
	}
	act(t, service, "next")
	if err := os.Remove(original); err != nil {
		t.Fatal(err)
	}
	previous := act(t, service, "previous")
	if previous.Current.Path != first.Current.Path || previous.HistoryCursor != 0 {
		t.Fatalf("cached history unavailable offline: %+v", previous)
	}
}

func TestServiceRematerializesHistoryAfterSameSizeCacheReplacement(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	service, _ := testService(t, backend)
	first := act(t, service, "next")
	act(t, service, "next")

	original, err := os.ReadFile(first.Current.Path)
	if err != nil {
		t.Fatal(err)
	}
	replaced := bytes.Clone(original)
	replaced[len(replaced)-1] ^= 1
	if err := os.WriteFile(first.Current.Path, replaced, 0600); err != nil {
		t.Fatal(err)
	}
	previous := act(t, service, "previous")
	restored, err := os.ReadFile(first.Current.Path)
	if err != nil || !bytes.Equal(restored, original) || previous.Current.Path != first.Current.Path || backend.calls != 3 {
		t.Fatalf("previous did not restore the original cache bytes and apply the entry: %+v %v", previous, err)
	}
}

func TestServiceRematerializesEvictedHistoryAndProtectsCurrent(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	service, root := testService(t, backend)
	service.cache.Close()
	bounded, err := cache.New(cache.Options{Dir: service.request.CachePath, MaxImages: 2, MaxBytes: 512 << 20})
	if err != nil {
		t.Fatal(err)
	}
	service.cache = bounded
	service.draw = func(n int) int { return n - 1 }
	target := "a.png"
	service.request.Source = changedProvider{page: func(page source.Page) source.Page {
		result := []source.Candidate{}
		for _, c := range page.Candidates {
			if filepath.Base(c.Content.Path) == target {
				result = append(result, c)
			}
		}
		page.Candidates = result
		return page
	}}
	first := act(t, service, "next")
	if err := os.Rename(filepath.Join(root, "a.png"), filepath.Join(root, "a.hidden")); err != nil {
		t.Fatal(err)
	}
	target = "b.png"
	act(t, service, "next")
	if err := os.Rename(filepath.Join(root, "b.png"), filepath.Join(root, "b.hidden")); err != nil {
		t.Fatal(err)
	}
	target = "c.png"
	third := act(t, service, "next")
	if _, err := os.Stat(first.Current.Path); !os.IsNotExist(err) {
		t.Fatalf("old cached image not evicted: %v", err)
	}
	if _, err := os.Stat(third.Current.Path); err != nil {
		t.Fatal("displayed image evicted")
	}
	for _, name := range []string{"a", "b"} {
		if err := os.Rename(filepath.Join(root, name+".hidden"), filepath.Join(root, name+".png")); err != nil {
			t.Fatal(err)
		}
	}
	act(t, service, "previous")
	restored := act(t, service, "previous")
	if restored.Current.Path != first.Current.Path || restored.HistoryLength != 3 || restored.HistoryCursor != 0 {
		t.Fatalf("eviction erased history or origin: %+v", restored)
	}
	for _, name := range []string{"a.png", "b.png", "c.png"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatal("cache eviction removed user original")
		}
	}
}

func TestServiceCapacityFailurePreservesAcceptedState(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	service, _ := testService(t, backend)
	service.cache.Close()
	bounded, err := cache.New(cache.Options{Dir: service.request.CachePath, MaxImages: 1, MaxBytes: 512 << 20})
	if err != nil {
		t.Fatal(err)
	}
	service.cache = bounded
	first := act(t, service, "next")
	status, err := service.Action(context.Background(), "next")
	if err == nil || status.HistoryLength != 1 || status.Uncertain || status.Current.Path != first.Current.Path || backend.calls != 1 {
		t.Fatalf("capacity failure changed desktop/state: %+v %v", status, err)
	}
	if _, err := os.Stat(first.Current.Path); err != nil {
		t.Fatal("capacity failure evicted displayed image")
	}
}
