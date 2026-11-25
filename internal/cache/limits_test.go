package cache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/source"
)

func TestUsageCountsSharedFilesOnceAndIgnoresUnmanagedFiles(t *testing.T) {
	c := newCache(t, 20, 0)
	original, data := fixture(t, 10)
	first, err := c.Materialize(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	alias := original
	alias.Path = filepath.Join(t.TempDir(), "alias.png")
	if err := os.WriteFile(alias.Path, data, 0600); err != nil {
		t.Fatal(err)
	}
	second, err := c.Materialize(t.Context(), alias)
	if err != nil {
		t.Fatal(err)
	}
	if second.Path != first.Path {
		t.Fatal("duplicate content created another file")
	}
	ignored := filepath.Join(c.options.Dir, "user-note.txt")
	if err := os.WriteFile(ignored, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := c.Usage(t.Context(), first.Path, first.Path)
	if err != nil || got.Usage.Images != 1 || got.Usage.Bytes != first.Bytes || got.Usage.ProtectedImages != 1 || got.Usage.ProtectedBytes != first.Bytes || got.Usage.State != "within_budget" {
		t.Fatalf("usage=%+v %v", got, err)
	}
	if _, err := c.UpdateLimits(t.Context(), 1, first.Bytes, first.Path); err != nil {
		t.Fatal(err)
	}
	exists(t, ignored, true)
	exists(t, original.Path, true)
	exists(t, alias.Path, true)
}

func TestRuntimeCountLimitsProtectCurrentAndPending(t *testing.T) {
	c := newCache(t, 20, 0)
	files := make([]Result, 3)
	for i := range files {
		original, _ := fixture(t, uint8(i+1))
		result, err := c.Materialize(t.Context(), original)
		if err != nil {
			t.Fatal(err)
		}
		files[i] = result
	}
	got, err := c.UpdateLimits(t.Context(), 1, DefaultMaxBytes, files[0].Path, files[1].Path)
	if err != nil || got.Limits.MaxImages != 1 || got.Usage.State != "protected_over_budget" || got.Usage.Images != 2 || got.Usage.ProtectedImages != 2 {
		t.Fatalf("protected usage=%+v %v", got, err)
	}
	exists(t, files[0].Path, true)
	exists(t, files[1].Path, true)
	exists(t, files[2].Path, false)
	got, err = c.UpdateLimits(t.Context(), 2, DefaultMaxBytes, files[0].Path, files[1].Path)
	if err != nil || got.Usage.State != "within_budget" {
		t.Fatalf("increase=%+v %v", got, err)
	}
}

func TestRuntimeByteLimitsPruneOldDisposableCopies(t *testing.T) {
	c := newCache(t, 20, 0)
	files := make([]Result, 3)
	for i := range files {
		original, _ := fixture(t, uint8(i+1))
		result, err := c.Materialize(t.Context(), original)
		if err != nil {
			t.Fatal(err)
		}
		files[i] = result
		stamp := time.Unix(int64(i+1), 0)
		if err := os.Chtimes(result.Path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	got, err := c.UpdateLimits(t.Context(), 20, files[1].Bytes+files[2].Bytes)
	if err != nil || got.Usage.Images != 2 || got.Usage.State != "within_budget" {
		t.Fatalf("byte eviction=%+v %v", got, err)
	}
	exists(t, files[0].Path, false)
	exists(t, files[1].Path, true)
	exists(t, files[2].Path, true)
	got, err = c.UpdateLimits(t.Context(), 20, files[2].Bytes-1, files[2].Path)
	if err != nil || got.Usage.Images != 1 || got.Usage.Bytes != files[2].Bytes || got.Usage.State != "protected_over_budget" {
		t.Fatalf("protected bytes=%+v %v", got, err)
	}
	exists(t, files[2].Path, true)
	original, _ := fixture(t, 4)
	if _, err := c.Materialize(t.Context(), original, files[2].Path); err == nil {
		t.Fatal("published over-budget content")
	}
	exists(t, files[2].Path, true)
}

func TestLimitValidationAndCleanupFailureKeepTruthfulActiveLimits(t *testing.T) {
	c := newCache(t, 20, 0)
	for _, limits := range []Limits{{0, 1}, {1, 0}, {-1, 10}} {
		got, err := c.UpdateLimits(t.Context(), limits.MaxImages, limits.MaxBytes)
		if err == nil || got.Limits.MaxImages != 20 || got.Limits.MaxBytes != DefaultMaxBytes {
			t.Fatalf("invalid update=%+v %v", got, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := c.UpdateLimits(ctx, 1, 100)
	if err == nil || got.Limits.MaxImages != 20 {
		t.Fatalf("canceled=%+v %v", got, err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	for i := 0; i < 2; i++ {
		original, _ := fixture(t, uint8(i+1))
		if _, err := c.Materialize(t.Context(), original); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(c.options.Dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(c.options.Dir, 0700) })
	got, err = c.UpdateLimits(t.Context(), 1, DefaultMaxBytes)
	if err == nil || got.Limits.MaxImages != 1 || got.Usage.Images != 2 || got.Usage.State != "over_budget" {
		t.Fatalf("cleanup failure hid active limits=%+v %v", got, err)
	}
	if err := os.Chmod(c.options.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	got, err = c.UpdateLimits(t.Context(), 1, DefaultMaxBytes)
	if err != nil || got.Usage.Images != 1 || got.Usage.State != "within_budget" {
		t.Fatalf("retry=%+v %v", got, err)
	}
}

func TestLimitUpdateWaitsForPublicationAndPreservesItsProtectedFile(t *testing.T) {
	c := newCache(t, 20, 0)
	original, _ := fixture(t, 1)
	old, err := c.Materialize(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	_, data := fixture(t, 2)
	digest := sha256.Sum256(data)
	nextPath := filepath.Join(c.options.Dir, hex.EncodeToString(digest[:])+".png")
	entered := make(chan struct{})
	released, release := context.WithCancel(t.Context())
	t.Cleanup(release)
	c.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		close(entered)
		select {
		case <-released.Done():
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, ContentLength: int64(len(data)), Body: io.NopCloser(bytes.NewReader(data)), Header: http.Header{}}, nil
	})}
	published := make(chan error, 1)
	go func() {
		_, err := c.Materialize(t.Context(), source.Content{MediaKind: "image", Kind: "https", URL: "https://images.example/image.png"})
		published <- err
	}()
	<-entered
	updating := make(chan struct{})
	updated := make(chan error, 1)
	go func() {
		close(updating)
		_, err := c.UpdateLimits(t.Context(), 1, DefaultMaxBytes, nextPath)
		updated <- err
	}()
	<-updating
	select {
	case err := <-updated:
		t.Fatalf("limits raced publication=%v", err)
	case <-time.After(20 * time.Millisecond):
	}
	exists(t, old.Path, true)
	release()
	if err := <-published; err != nil {
		t.Fatal(err)
	}
	if err := <-updated; err != nil {
		t.Fatal(err)
	}
	exists(t, old.Path, false)
	exists(t, nextPath, true)
}
