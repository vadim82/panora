// Package cache materializes selected originals into a bounded, host-owned cache.
package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/vadim82/panora/internal/filelock"
	"github.com/vadim82/panora/internal/localfiles"
	"github.com/vadim82/panora/internal/source"
	_ "golang.org/x/image/webp"
)

const (
	DefaultMaxImages       = 20
	DefaultMaxBytes  int64 = 512 << 20
	MaxImageBytes    int64 = 128 << 20
	MaxPixels              = 64_000_000
)

type Options struct {
	Dir       string
	MaxImages int
	MaxBytes  int64
}
type Result struct {
	Path, Hash    string
	Width, Height int
	Bytes         int64
}
type Cache struct {
	options Options
	client  *http.Client
	// Writers and Close acquire writeMu before mu. Staging can hold only
	// writeMu so previews of published files stay available during downloads.
	writeMu  sync.Mutex
	mu       sync.Mutex
	release  func()
	previews previewCache
}

// Close waits for filesystem work and releases exclusive cache ownership.
func (c *Cache) Close() {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.release != nil {
		c.client.CloseIdleConnections()
		c.release()
		c.release = nil
		c.previews = previewCache{}
	}
}

// Reuse verifies the bytes of a previously validated cache record and refreshes
// its recency. Missing or invalid records can be materialized from their origin.
func (c *Cache) Reuse(ctx context.Context, result Result, protected ...string) (Result, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if result.Width < 1 || result.Height < 1 || result.Width > MaxPixels/result.Height {
		return Result{}, fmt.Errorf("invalid cached image dimensions")
	}
	f, n, err := c.openVerifiedCacheFile(ctx, result.Path, result.Hash)
	if err != nil {
		return Result{}, err
	}
	defer f.Close()
	if n != result.Bytes {
		return Result{}, fmt.Errorf("cached image size differs from validated metadata")
	}
	// A header mismatch means the recorded dimensions cannot be trusted.
	config, format, err := inspectImage(ctx, f, n)
	if err != nil {
		return Result{}, err
	}
	if filepath.Ext(result.Path) != imageExtension(format) {
		return Result{}, fmt.Errorf("cached image format changed")
	}
	if config.Width != result.Width || config.Height != result.Height {
		pixels, _, err := decodeImage(ctx, f, n, nil)
		if err != nil {
			return Result{}, err
		}
		result.Width, result.Height = pixels.Bounds().Dx(), pixels.Bounds().Dy()
	}
	result.Path = filepath.Clean(result.Path)
	entries, err := c.entries()
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := c.makeRoom(entries, result, protected); err != nil {
		return Result{}, err
	}
	now := time.Now()
	if err := os.Chtimes(result.Path, now, now); err != nil {
		return Result{}, err
	}
	return result, nil
}

// VerifyAccepted checks an already validated wallpaper by its recorded hash.
// Reapplying the same bytes does not require decoding or updating cache recency.
func (c *Cache) VerifyAccepted(ctx context.Context, path, expectedHash string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	f, _, err := c.openVerifiedCacheFile(ctx, path, expectedHash)
	if err != nil {
		return err
	}
	return f.Close()
}

// openVerifiedCacheFile is called with the cache lock held. The caller owns the
// returned descriptor on success.
func (c *Cache) openVerifiedCacheFile(ctx context.Context, path, expectedHash string) (*os.File, int64, error) {
	if c.release == nil {
		return nil, 0, fmt.Errorf("playback cache is closed")
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	ext := filepath.Ext(path)
	if filepath.Dir(filepath.Clean(path)) != filepath.Clean(c.options.Dir) || !filepath.IsAbs(path) || len(expectedHash) != 64 || !isCacheExtension(ext) || filepath.Base(path) != expectedHash+ext {
		return nil, 0, fmt.Errorf("invalid cached content identity")
	}
	if _, err := hex.DecodeString(expectedHash); err != nil {
		return nil, 0, fmt.Errorf("invalid cached content hash")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, 0, fmt.Errorf("inspect cached image: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, 0, fmt.Errorf("cached image must be a regular file")
	}
	f, err := localfiles.OpenRegular(path)
	if err != nil {
		return nil, 0, err
	}
	limit := min(c.options.MaxBytes, MaxImageBytes)
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(contextReader{ctx, f}, limit+1))
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	if n < 1 || n > limit || hex.EncodeToString(h.Sum(nil)) != expectedHash {
		f.Close()
		return nil, 0, fmt.Errorf("cached image exceeds limits or its content hash changed")
	}
	if err := ctx.Err(); err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, n, nil
}

func imageExtension(format string) string {
	switch format {
	case "jpeg":
		return ".jpg"
	case "png":
		return ".png"
	case "webp":
		return ".webp"
	}
	return ""
}

func isCacheExtension(ext string) bool {
	return ext == ".jpg" || ext == ".png" || ext == ".webp"
}

func inspectImage(ctx context.Context, f io.ReadSeeker, size int64) (image.Config, string, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return image.Config{}, "", err
	}
	config, format, err := image.DecodeConfig(io.LimitReader(contextReader{ctx, f}, size))
	if err != nil {
		return image.Config{}, "", fmt.Errorf("inspect playback image: %w", err)
	}
	if imageExtension(format) == "" || config.Width < 1 || config.Height < 1 || config.Width > MaxPixels/config.Height {
		return image.Config{}, "", fmt.Errorf("unsupported format or image exceeds %d pixels", MaxPixels)
	}
	return config, format, nil
}

// Check the header before allocating pixels. Preview supplies the validated
// record so a replaced header cannot authorize a larger allocation.
func decodeImage(ctx context.Context, f io.ReadSeeker, size int64, known *Result) (image.Image, string, error) {
	config, format, err := inspectImage(ctx, f, size)
	if err != nil {
		return nil, "", err
	}
	if known != nil && (config.Width != known.Width || config.Height != known.Height || imageExtension(format) != filepath.Ext(known.Path)) {
		return nil, "", fmt.Errorf("image no longer matches validated metadata")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, "", err
	}
	decoded, decodedFormat, err := image.Decode(io.LimitReader(contextReader{ctx, f}, size))
	if err != nil {
		return nil, "", fmt.Errorf("decode playback image: %w", err)
	}
	if decodedFormat != format || decoded.Bounds().Size() != image.Pt(config.Width, config.Height) {
		return nil, "", fmt.Errorf("image format or dimensions changed while decoding")
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	return decoded, format, nil
}

// New reconciles incomplete staging files. Published content remains reusable
// without a database record; missing files can be materialized again by origin.
func New(options Options) (*Cache, error) {
	if options.MaxImages == 0 {
		options.MaxImages = DefaultMaxImages
	}
	if options.MaxBytes == 0 {
		options.MaxBytes = DefaultMaxBytes
	}
	if !filepath.IsAbs(options.Dir) || options.MaxImages < 1 || options.MaxBytes < 1 {
		return nil, fmt.Errorf("invalid playback cache options")
	}
	if err := os.MkdirAll(options.Dir, 0700); err != nil {
		return nil, fmt.Errorf("create playback cache: %w", err)
	}
	info, err := os.Lstat(options.Dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("playback cache must be a directory, not a symlink")
	}
	c := &Cache{options: options, client: secureClient()}
	release, err := filelock.Try(filepath.Join(options.Dir, ".cache.lock"))
	if err != nil {
		return nil, fmt.Errorf("lock playback cache: %w", err)
	}
	owned := false
	defer func() {
		if !owned {
			release()
		}
	}()
	entries, err := os.ReadDir(options.Dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".stage-") && entry.Type().IsRegular() {
			if err := os.Remove(filepath.Join(options.Dir, entry.Name())); err != nil {
				return nil, err
			}
		}
	}
	c.release = release
	owned = true
	return c, nil
}

// Materialize copies/downloads only this selected image. Protected paths include
// the displayed image and any pending desktop application. Originals are never
// modified. The returned filename is independent of provider and input identity.
func (c *Cache) Materialize(ctx context.Context, content source.Content, protected ...string) (Result, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.release == nil {
		return Result{}, fmt.Errorf("playback cache is closed")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	r, err := c.open(ctx, content)
	if err != nil {
		var typed *Error
		if errors.As(err, &typed) {
			return Result{}, err
		}
		return Result{}, &Error{Stage: "download", Code: "download_failed", Err: err}
	}
	defer r.Close()
	f, err := os.CreateTemp(c.options.Dir, ".stage-")
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	hash := sha256.New()
	limit := min(c.options.MaxBytes, MaxImageBytes)
	n, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(contextReader{ctx, r}, limit+1))
	if err != nil {
		return Result{}, &Error{Stage: "download", Code: "download_failed", Err: err}
	}
	if n > limit {
		return Result{}, &Error{Stage: "decode", Code: "invalid_content", Err: fmt.Errorf("selected image exceeds the %d-byte limit", limit)}
	}
	pixels, format, err := decodeImage(ctx, f, n, nil)
	if err != nil {
		return Result{}, &Error{Stage: "decode", Code: "invalid_content", Err: err}
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	ext := imageExtension(format)
	bounds := pixels.Bounds()
	result := Result{Path: filepath.Join(c.options.Dir, digest+ext), Hash: digest, Width: bounds.Dx(), Height: bounds.Dy(), Bytes: n}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	entries, err := c.entries()
	if err != nil {
		return Result{}, err
	}
	for _, entry := range entries {
		if entry.path == result.Path && entry.size == n && matchesHash(ctx, result.Path, digest, limit) {
			if err := c.makeRoom(entries, result, protected); err != nil {
				return Result{}, err
			}
			now := time.Now()
			if err := os.Chtimes(result.Path, now, now); err != nil {
				return Result{}, err
			}
			return result, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	for _, path := range protected {
		if filepath.Clean(path) == result.Path {
			return Result{}, fmt.Errorf("protected cached image is corrupt")
		}
	}
	if err := c.makeRoom(entries, result, protected); err != nil {
		return Result{}, err
	}
	if err := f.Sync(); err != nil {
		return Result{}, err
	}
	if err := f.Close(); err != nil {
		return Result{}, err
	}
	if err := os.Rename(f.Name(), result.Path); err != nil {
		return Result{}, fmt.Errorf("publish playback image: %w", err)
	}
	dir, err := os.Open(c.options.Dir)
	if err != nil {
		return Result{}, err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return Result{}, err
	}
	return result, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func (c *Cache) open(ctx context.Context, content source.Content) (io.ReadCloser, error) {
	if content.MediaKind != "image" {
		return nil, fmt.Errorf("playback cache requires image content")
	}
	switch content.Kind {
	case "local":
		if !filepath.IsAbs(content.Path) || content.URL != "" {
			return nil, fmt.Errorf("invalid local original")
		}
		f, err := localfiles.OpenRegular(content.Path)
		if err != nil {
			return nil, fmt.Errorf("open local original: %w", err)
		}
		return f, nil
	case "https":
		if content.Path != "" {
			return nil, fmt.Errorf("invalid remote original")
		}
		if err := validateURL(content.URL); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, content.URL, nil)
		if err != nil {
			return nil, err
		}
		response, err := c.client.Do(req)
		if err != nil {
			return nil, &Error{Stage: "download", Code: "download_failed", Err: err}
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			code := "unavailable"
			switch response.StatusCode {
			case 401:
				code = "auth_required"
			case 403:
				code = "auth_rejected"
			case 404:
				code = "not_found"
			case 429:
				code = "rate_limited"
			}
			return nil, &source.Error{Code: code, Message: "original download failed", RetryAfterSeconds: source.RetryAfter(response.Header.Get("Retry-After"), time.Now())}
		}
		if response.ContentLength > min(c.options.MaxBytes, MaxImageBytes) {
			response.Body.Close()
			return nil, &Error{Stage: "decode", Code: "invalid_content", Err: fmt.Errorf("remote original exceeds image byte limit")}
		}
		return response.Body, nil
	default:
		return nil, fmt.Errorf("unsupported original location")
	}
}

type diskEntry struct {
	path string
	size int64
	used time.Time
}

func (c *Cache) entries() ([]diskEntry, error) {
	entries, err := os.ReadDir(c.options.Dir)
	if err != nil {
		return nil, err
	}
	var out []diskEntry
	for _, entry := range entries {
		name := entry.Name()
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		if len(base) != 64 || !isCacheExtension(ext) {
			continue
		}
		if _, err := hex.DecodeString(base); err != nil {
			continue
		}
		if !entry.Type().IsRegular() {
			return nil, fmt.Errorf("nonregular file in playback cache")
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, diskEntry{filepath.Join(c.options.Dir, name), info.Size(), info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].used.Before(out[j].used) })
	return out, nil
}

// ErrCapacity indicates that an image cannot fit without evicting a protected path.
var ErrCapacity = errors.New("protected playback images leave insufficient cache capacity")

func (c *Cache) makeRoom(entries []diskEntry, result Result, protected []string) error {
	keep := map[string]bool{}
	for _, path := range protected {
		keep[filepath.Clean(path)] = true
	}
	var total int64
	count := len(entries)
	for _, entry := range entries {
		total += entry.size
	}
	// A corrupt existing copy with this digest is replaced without double counting.
	for _, entry := range entries {
		if entry.path == result.Path {
			total -= entry.size
			count--
		}
	}
	var victims []string
	for _, entry := range entries {
		if count+1 <= c.options.MaxImages && total+result.Bytes <= c.options.MaxBytes {
			break
		}
		if entry.path == result.Path || keep[entry.path] {
			continue
		}
		victims = append(victims, entry.path)
		count--
		total -= entry.size
	}
	if count+1 > c.options.MaxImages || total+result.Bytes > c.options.MaxBytes {
		return ErrCapacity
	}
	for _, path := range victims {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("evict cached image: %w", err)
		}
	}
	return nil
}
func matchesHash(ctx context.Context, path, want string, limit int64) bool {
	f, err := localfiles.OpenRegular(path)
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(contextReader{ctx, f}, limit+1))
	return err == nil && n <= limit && hex.EncodeToString(h.Sum(nil)) == want
}
