package cache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/source"
)

func TestPreviewRetainsAspectRatioBoundsAndAlphaWithoutEnlarging(t *testing.T) {
	for _, tc := range []struct {
		name, format                         string
		width, height, wantWidth, wantHeight int
	}{
		{"wide", "png", 1200, 300, 512, 128},
		{"tall", "jpeg", 300, 1200, 128, 512},
		{"small", "png", 8, 4, 8, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newCache(t, 2, 512<<20)
			original := image.NewNRGBA(image.Rect(0, 0, tc.width, tc.height))
			for y := 0; y < tc.height; y++ {
				for x := 0; x < tc.width; x++ {
					original.SetNRGBA(x, y, color.NRGBA{R: 200, G: uint8(x), B: uint8(y), A: 180})
				}
			}
			var encoded bytes.Buffer
			var err error
			ext := ".png"
			if tc.format == "jpeg" {
				err = jpeg.Encode(&encoded, original, nil)
				ext = ".jpg"
			} else {
				err = png.Encode(&encoded, original)
			}
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "original"+ext)
			if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			known, err := c.Materialize(t.Context(), source.Content{Kind: "local", MediaKind: "image", Path: path})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(known.Path, time.Unix(1, 0), time.Unix(1, 0)); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadDir(c.options.Dir)
			if err != nil {
				t.Fatal(err)
			}
			preview, err := c.Preview(t.Context(), known)
			if err != nil || preview.Width != tc.wantWidth || preview.Height != tc.wantHeight || len(preview.Data) > MaxPreviewBytes {
				t.Fatalf("preview=%+v %v", preview, err)
			}
			decoded, format, err := image.Decode(bytes.NewReader(preview.Data))
			if err != nil || format != "png" || decoded.Bounds().Dx() != preview.Width || decoded.Bounds().Dy() != preview.Height {
				t.Fatalf("decoded=%v format=%s error=%v", decoded, format, err)
			}
			if tc.format == "png" {
				_, _, _, alpha := decoded.At(0, 0).RGBA()
				if alpha == 65535 {
					t.Fatal("preview discarded alpha")
				}
			}
			after, err := os.ReadDir(c.options.Dir)
			if err != nil || len(before) != len(after) {
				t.Fatal("preview created cache files")
			}
			info, err := os.Stat(known.Path)
			if err != nil || !info.ModTime().Equal(time.Unix(1, 0)) {
				t.Fatal("preview changed cache recency")
			}
		})
	}
}

func TestPreviewRejectsUnavailableUntrustedOrOversizedContent(t *testing.T) {
	for _, mode := range []string{"missing", "corrupt", "symlink", "outside", "wrong dimensions", "wrong extension", "too many pixels", "too many bytes"} {
		t.Run(mode, func(t *testing.T) {
			c := newCache(t, 2, 512<<20)
			original, data := fixture(t, 20)
			known, err := c.Materialize(t.Context(), original)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.Preview(t.Context(), known); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "missing":
				err = os.Remove(known.Path)
			case "corrupt":
				err = os.WriteFile(known.Path, bytes.Repeat([]byte{'x'}, len(data)), 0600)
			case "symlink":
				if err = os.Remove(known.Path); err == nil {
					err = os.Symlink(original.Path, known.Path)
				}
			case "outside":
				known.Path = original.Path
			case "wrong dimensions":
				known.Width++
			case "wrong extension":
				renamed := known.Path[:len(known.Path)-4] + ".jpg"
				err = os.Rename(known.Path, renamed)
				known.Path = renamed
			case "too many pixels":
				known.Width = MaxPixels
				known.Height = 2
			case "too many bytes":
				known.Bytes = MaxImageBytes + 1
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.Preview(t.Context(), known); !errors.Is(err, ErrPreviewUnavailable) {
				t.Fatalf("untrusted preview accepted: %v", err)
			}
		})
	}
}

func TestPreviewChecksDecodedHeaderBeforeAllocatingAnOversizedImage(t *testing.T) {
	c := newCache(t, 2, 512<<20)
	_, data := fixture(t, 20)
	// This cache record claims tiny dimensions, but the PNG header claims a large
	// image. Validating the checksum alone must not permit the large allocation.
	binaryData := append([]byte(nil), data...)
	binary.BigEndian.PutUint32(binaryData[16:20], MaxPixels)
	binary.BigEndian.PutUint32(binaryData[20:24], 2)
	binary.BigEndian.PutUint32(binaryData[29:33], crc32.ChecksumIEEE(binaryData[12:29]))
	digest := sha256.Sum256(binaryData)
	known := Result{Hash: hex.EncodeToString(digest[:]), Bytes: int64(len(binaryData)), Width: 8, Height: 4}
	known.Path = filepath.Join(c.options.Dir, known.Hash+".png")
	if err := os.WriteFile(known.Path, binaryData, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Preview(t.Context(), known); !errors.Is(err, ErrPreviewUnavailable) {
		t.Fatalf("oversized header accepted=%v", err)
	}
}

func TestPreviewCancellationBusyCacheAndEncodedSizeLimit(t *testing.T) {
	c := newCache(t, 2, 512<<20)
	original, _ := fixture(t, 20)
	known, err := c.Materialize(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Preview(canceled, known); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	if err := c.Validate(canceled, known); !errors.Is(err, context.Canceled) {
		t.Fatalf("validation cancellation=%v", err)
	}
	deadline, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	if _, err := c.Preview(deadline, known); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout=%v", err)
	}
	c.mu.Lock()
	_, err = c.Preview(t.Context(), known)
	validationErr := c.Validate(t.Context(), known)
	c.mu.Unlock()
	if !errors.Is(err, ErrPreviewBusy) {
		t.Fatalf("busy preview waited=%v", err)
	}
	if !errors.Is(validationErr, ErrPreviewBusy) {
		t.Fatalf("busy validation waited=%v", validationErr)
	}
	w := &previewWriter{ctx: t.Context()}
	if _, err := w.Write(make([]byte, MaxPreviewBytes)); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte{'x'}); !errors.Is(err, ErrPreviewUnavailable) || w.Len() != MaxPreviewBytes {
		t.Fatalf("encoded size limit=%d %v", w.Len(), err)
	}
	c.Close()
	if _, err := c.Preview(t.Context(), known); !errors.Is(err, ErrPreviewUnavailable) {
		t.Fatalf("closed cache=%v", err)
	}
}

func TestPreviewReusesPrivateBytesAndSeparatesSizes(t *testing.T) {
	c := newCache(t, 2, 512<<20)
	original, _ := fixture(t, 20)
	known, err := c.Materialize(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	first, err := c.Preview(t.Context(), known)
	if err != nil {
		t.Fatal(err)
	}
	saved := bytes.Clone(first.Data)
	first.Data[0] ^= 0xff
	entry := c.previews.entries[0]
	warm, err := c.Preview(t.Context(), known)
	if err != nil || !bytes.Equal(warm.Data, saved) || &entry.preview.Data[0] != &c.previews.entries[0].preview.Data[0] {
		t.Fatalf("warm preview failed to reuse cached PNG: %v", err)
	}
	warm.Data[0] ^= 0xff
	smaller, err := c.preview(t.Context(), known, 1)
	if err != nil || smaller.Width != 1 || smaller.Height != 1 || bytes.Equal(smaller.Data, saved) || len(c.previews.entries) != 2 {
		t.Fatalf("smaller preview = %dx%d, %v", smaller.Width, smaller.Height, err)
	}
	again, err := c.Preview(t.Context(), known)
	if err != nil || !bytes.Equal(again.Data, saved) {
		t.Fatalf("larger preview failed after requesting a different size: %v", err)
	}
}

func TestThumbnailCacheBoundsAndRebuildsEvictedPreview(t *testing.T) {
	c := newCache(t, 2, 512<<20)
	original, _ := fixture(t, 20)
	known, err := c.Materialize(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	first, err := c.Preview(t.Context(), known)
	if err != nil {
		t.Fatal(err)
	}
	firstKey := previewKey{known: known, edge: MaxPreviewDimension}
	for edge := 1; edge <= maxPreviewCacheEntries; edge++ {
		if _, err := c.preview(t.Context(), known, edge); err != nil {
			t.Fatal(err)
		}
	}
	if c.previews.find(firstKey) >= 0 || len(c.previews.entries) != maxPreviewCacheEntries {
		t.Fatal("cache retained the oldest entry past its count limit")
	}
	rebuilt, err := c.Preview(t.Context(), known)
	if err != nil || !bytes.Equal(first.Data, rebuilt.Data) || c.previews.find(firstKey) < 0 {
		t.Fatalf("evicted preview was not rebuilt: %v", err)
	}

	large := Preview{Data: make([]byte, 1, MaxPreviewBytes)}
	var entries previewCache
	for edge := 1; edge <= maxPreviewCacheEntries; edge++ {
		entries.remember(previewKey{edge: edge}, large)
	}
	if len(entries.entries) != maxPreviewCacheBytes/MaxPreviewBytes || entries.bytes != maxPreviewCacheBytes {
		t.Fatalf("cache limits: entries=%d bytes=%d", len(entries.entries), entries.bytes)
	}
}

func BenchmarkLargeWallpaperPreview(b *testing.B) {
	c, err := New(Options{Dir: b.TempDir()})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(c.Close)
	original := image.NewRGBA(image.Rect(0, 0, 7680, 2160))
	for y := 0; y < original.Bounds().Dy(); y++ {
		for x := 0; x < original.Bounds().Dx(); x++ {
			original.SetRGBA(x, y, color.RGBA{R: uint8(x / 9), G: uint8(y / 7), B: uint8((x+y)/13 + (x*y)%31), A: 255})
		}
	}
	path := filepath.Join(b.TempDir(), "large.jpg")
	file, err := os.Create(path)
	if err != nil {
		b.Fatal(err)
	}
	err = jpeg.Encode(file, original, &jpeg.Options{Quality: 90})
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		b.Fatal(errors.Join(err, closeErr))
	}
	known, err := c.Materialize(b.Context(), source.Content{Kind: "local", MediaKind: "image", Path: path})
	if err != nil {
		b.Fatal(err)
	}
	b.Logf("cached JPEG: %d bytes", known.Bytes)
	for _, name := range []string{"cold", "repeated"} {
		b.Run(name, func(b *testing.B) {
			if _, err := c.Preview(b.Context(), known); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				if name == "cold" {
					c.previews = previewCache{}
				}
				if _, err := c.Preview(b.Context(), known); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
