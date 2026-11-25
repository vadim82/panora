package cache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/vadim82/panora/internal/localfiles"
	"golang.org/x/image/draw"
)

const (
	MaxPreviewDimension = 512
	MaxPreviewBytes     = 2 << 20
	PreviewTimeout      = 3 * time.Second
)

var ErrPreviewBusy = errors.New("playback cache is busy")
var ErrPreviewUnavailable = errors.New("validated preview content is unavailable")

type Preview struct {
	Width, Height int
	Data          []byte
}

const (
	maxPreviewCacheEntries = 64
	maxPreviewCacheBytes   = 16 << 20
)

type previewKey struct {
	known Result
	edge  int
}

type previewEntry struct {
	key     previewKey
	preview Preview
}

// Entries are ordered from least to most recently used. Cache.mu protects all
// access. The bytes are private to the cache.
type previewCache struct {
	entries []previewEntry
	bytes   int
}

func (c *previewCache) find(key previewKey) int {
	return slices.IndexFunc(c.entries, func(entry previewEntry) bool { return entry.key == key })
}

func (c *previewCache) remove(index int) {
	c.bytes -= cap(c.entries[index].preview.Data)
	c.entries = slices.Delete(c.entries, index, index+1)
}

func (c *previewCache) remember(key previewKey, preview Preview) {
	for len(c.entries) >= maxPreviewCacheEntries || c.bytes+cap(preview.Data) > maxPreviewCacheBytes {
		c.remove(0)
	}
	c.entries = append(c.entries, previewEntry{key: key, preview: preview})
	c.bytes += cap(preview.Data)
}

// Preview reads only a known, hash-named cache file. Holding mu through the
// complete read/encode protects it from eviction and runtime budget edits.
// No full-size cache file is downloaded, published, evicted or touched here.
func (c *Cache) Preview(ctx context.Context, known Result) (Preview, error) {
	return c.preview(ctx, known, MaxPreviewDimension)
}

func (c *Cache) preview(ctx context.Context, known Result, edge int) (Preview, error) {
	if err := ctx.Err(); err != nil {
		return Preview{}, err
	}
	if !c.mu.TryLock() {
		return Preview{}, ErrPreviewBusy
	}
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, PreviewTimeout)
	defer cancel()

	key := previewKey{known: known, edge: edge}
	index := c.previews.find(key)
	file, digest, err := c.openKnownImage(known)
	if err != nil {
		if index >= 0 {
			c.previews.remove(index)
		}
		return Preview{}, err
	}
	defer file.Close()
	if index >= 0 {
		if err := verifyKnownImage(ctx, file, known.Bytes, digest); err != nil {
			if errors.Is(err, ErrPreviewUnavailable) {
				c.previews.remove(index)
			}
			return Preview{}, err
		}
		entry := c.previews.entries[index]
		copy(c.previews.entries[index:], c.previews.entries[index+1:])
		c.previews.entries[len(c.previews.entries)-1] = entry
		return clonePreview(entry.preview), nil
	}

	// Decode the exact bytes that passed the hash check. An external write
	// between verification and decoding must not seed the cache for this hash.
	var verified bytes.Buffer
	verified.Grow(int(known.Bytes) + 1)
	if err := verifyKnownImage(ctx, io.TeeReader(file, &verified), known.Bytes, digest); err != nil {
		return Preview{}, err
	}
	original, _, err := decodeImage(ctx, bytes.NewReader(verified.Bytes()), known.Bytes, &known)
	if err != nil {
		return Preview{}, previewReadError(ctx, err)
	}
	thumbnail := resizeThumbnail(original, edge)
	output := &previewWriter{ctx: ctx}
	if err := png.Encode(output, thumbnail); err != nil {
		return Preview{}, previewReadError(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return Preview{}, err
	}
	preview := Preview{Width: thumbnail.Bounds().Dx(), Height: thumbnail.Bounds().Dy(), Data: output.Bytes()}
	c.previews.remember(key, clonePreview(preview))
	return preview, nil
}

func clonePreview(preview Preview) Preview {
	preview.Data = bytes.Clone(preview.Data)
	return preview
}

// Validate fully checks known cached bytes without generating a thumbnail or
// changing recency. The cache lock protects the entire read from eviction.
func (c *Cache) Validate(ctx context.Context, known Result) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !c.mu.TryLock() {
		return ErrPreviewBusy
	}
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, PreviewTimeout)
	defer cancel()
	file, digest, err := c.openKnownImage(known)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := verifyKnownImage(ctx, file, known.Bytes, digest); err != nil {
		return err
	}
	if _, _, err := decodeImage(ctx, file, known.Bytes, &known); err != nil {
		return previewReadError(ctx, err)
	}
	return nil
}

// Called under mu. Only a regular file matching the recorded identity and size
// can supply bytes to the bounded verifier.
func (c *Cache) openKnownImage(known Result) (*os.File, []byte, error) {
	if c.release == nil {
		return nil, nil, ErrPreviewUnavailable
	}
	ext := filepath.Ext(known.Path)
	digest, err := hex.DecodeString(known.Hash)
	if err != nil || len(digest) != sha256.Size || filepath.Dir(filepath.Clean(known.Path)) != filepath.Clean(c.options.Dir) || !filepath.IsAbs(known.Path) || !isCacheExtension(ext) || filepath.Base(known.Path) != known.Hash+ext {
		return nil, nil, ErrPreviewUnavailable
	}
	if known.Bytes < 1 || known.Bytes > MaxImageBytes || known.Width < 1 || known.Height < 1 || known.Width > MaxPixels/known.Height {
		return nil, nil, ErrPreviewUnavailable
	}
	info, err := os.Lstat(known.Path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != known.Bytes {
		return nil, nil, ErrPreviewUnavailable
	}
	file, err := localfiles.OpenRegular(known.Path)
	if err != nil {
		return nil, nil, ErrPreviewUnavailable
	}
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != known.Bytes {
		file.Close()
		return nil, nil, ErrPreviewUnavailable
	}
	return file, digest, nil
}

func verifyKnownImage(ctx context.Context, reader io.Reader, expectedBytes int64, digest []byte) error {
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(contextReader{ctx, reader}, expectedBytes+1))
	if err != nil {
		return previewReadError(ctx, err)
	}
	if size != expectedBytes || !bytes.Equal(hash.Sum(nil), digest) {
		return ErrPreviewUnavailable
	}
	return ctx.Err()
}

func previewReadError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fmt.Errorf("%w: %v", ErrPreviewUnavailable, err)
}

func resizeThumbnail(original image.Image, edge int) *image.NRGBA {
	bounds := original.Bounds()
	width, height := thumbnailSize(bounds.Dx(), bounds.Dy(), edge)
	thumbnail := image.NewNRGBA(image.Rect(0, 0, width, height))
	// Approximate bilinear sampling bounds work by the output dimensions.
	draw.ApproxBiLinear.Scale(thumbnail, thumbnail.Bounds(), original, bounds, draw.Src, nil)
	return thumbnail
}

func thumbnailSize(width, height, edge int) (int, int) {
	if width <= edge && height <= edge {
		return width, height
	}
	if width >= height {
		return edge, max(1, int(int64(height)*int64(edge)/int64(width)))
	}
	return max(1, int(int64(width)*int64(edge)/int64(height))), edge
}

type previewWriter struct {
	bytes.Buffer
	ctx context.Context
}

func (w *previewWriter) Write(data []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if len(data) > MaxPreviewBytes-w.Len() {
		return 0, ErrPreviewUnavailable
	}
	return w.Buffer.Write(data)
}

// DiscoverThumbnail reads a local original as part of source discovery. It
// returns decoded geometry and a small thumbnail. Serving a preview uses these
// prepared bytes and never invokes this function.
func DiscoverThumbnail(ctx context.Context, path string) (Preview, string, error) {
	file, err := localfiles.OpenRegular(path)
	if err != nil {
		return Preview{}, "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() < 1 || info.Size() > MaxImageBytes {
		return Preview{}, "", ErrPreviewUnavailable
	}
	original, _, err := decodeImage(ctx, file, info.Size(), nil)
	if err != nil {
		return Preview{}, "", previewReadError(ctx, err)
	}
	output := &previewWriter{ctx: ctx}
	if err := jpeg.Encode(output, resizeThumbnail(original, 180), &jpeg.Options{Quality: 76}); err != nil {
		return Preview{}, "", err
	}
	if output.Len() > 64<<10 {
		return Preview{}, "", ErrPreviewUnavailable
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return Preview{}, "", err
	}
	digest := sha256.New()
	if size, err := io.Copy(digest, io.LimitReader(contextReader{ctx, file}, info.Size()+1)); err != nil || size != info.Size() {
		return Preview{}, "", ErrPreviewUnavailable
	}
	bounds := original.Bounds()
	return Preview{Width: bounds.Dx(), Height: bounds.Dy(), Data: output.Bytes()}, hex.EncodeToString(digest.Sum(nil)), nil
}
