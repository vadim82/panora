package cache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/source"
)

func fixture(t *testing.T, shade uint8) (source.Content, []byte) {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, 8, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 8; x++ {
			im.Set(x, y, color.NRGBA{shade, uint8(x), uint8(y), 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "original.png")
	if err := os.WriteFile(p, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return source.Content{MediaKind: "image", Kind: "local", Path: p}, b.Bytes()
}
func newCache(t *testing.T, count int, size int64) *Cache {
	t.Helper()
	c, err := New(Options{Dir: filepath.Join(t.TempDir(), "cache"), MaxImages: count, MaxBytes: size})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}
func exists(t *testing.T, path string, want bool) {
	t.Helper()
	_, err := os.Stat(path)
	if (err == nil) != want {
		t.Fatalf("exists(%s)=%v, want %v", path, err == nil, want)
	}
}

func TestLocalIndependentCopyAndContentReuse(t *testing.T) {
	c := newCache(t, 0, 0)
	original, data := fixture(t, 20)
	got, err := c.Materialize(context.Background(), original)
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != 8 || got.Height != 4 || got.Bytes != int64(len(data)) || got.Path == original.Path {
		t.Fatalf("bad result %+v", got)
	}
	digest := sha256.Sum256(data)
	if got.Hash != hex.EncodeToString(digest[:]) {
		t.Fatal("wrong content hash")
	}
	copyPath := filepath.Join(t.TempDir(), "alias.png")
	if err := os.WriteFile(copyPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	alias := original
	alias.Path = copyPath
	second, err := c.Materialize(context.Background(), alias, got.Path)
	if err != nil {
		t.Fatal(err)
	}
	if second.Path != got.Path {
		t.Fatal("identical content not reused")
	}
	if err := os.Remove(original.Path); err != nil {
		t.Fatal(err)
	}
	cached, err := os.ReadFile(got.Path)
	if err != nil || !bytes.Equal(cached, data) {
		t.Fatal("cache depends on original")
	}
	entries, err := c.entries()
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries %v %v", entries, err)
	}
}

func TestEvictionProtectsDisplayedAndRejectsOvercommit(t *testing.T) {
	c := newCache(t, 2, 0)
	a, _ := fixture(t, 10)
	b, _ := fixture(t, 20)
	d, _ := fixture(t, 30)
	ra, err := c.Materialize(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	rb, err := c.Materialize(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Materialize(context.Background(), d, ra.Path, rb.Path); err == nil {
		t.Fatal("accepted overcommit with two protected images")
	}
	exists(t, ra.Path, true)
	exists(t, rb.Path, true)
	rd, err := c.Materialize(context.Background(), d, ra.Path)
	if err != nil {
		t.Fatal(err)
	}
	exists(t, ra.Path, true)
	exists(t, rb.Path, false)
	exists(t, rd.Path, true)
	exists(t, a.Path, true)
	exists(t, b.Path, true)
	entries, err := os.ReadDir(c.options.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".stage-") {
			t.Fatal("temporary stage survived")
		}
	}
}

func TestByteBudgetAndInvalidFilesLeaveAcceptedImage(t *testing.T) {
	a, data := fixture(t, 11)
	b, _ := fixture(t, 12)
	c := newCache(t, 20, int64(len(data)+10))
	ra, err := c.Materialize(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Materialize(context.Background(), b, ra.Path); err == nil {
		t.Fatal("byte budget exceeded")
	}
	exists(t, ra.Path, true)
	for _, contents := range [][]byte{[]byte("not an image"), data[:len(data)/2], bytes.Repeat([]byte("x"), len(data)+11)} {
		path := filepath.Join(t.TempDir(), "bad.png")
		if err := os.WriteFile(path, contents, 0600); err != nil {
			t.Fatal(err)
		}
		bad := a
		bad.Path = path
		if _, err := c.Materialize(context.Background(), bad, ra.Path); err == nil {
			t.Fatal("invalid file accepted")
		}
		exists(t, ra.Path, true)
	}
	missing := a
	missing.Path = filepath.Join(t.TempDir(), "missing.png")
	if _, err := c.Materialize(context.Background(), missing); err == nil {
		t.Fatal("missing source accepted")
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRemoteMaterializationAndBounds(t *testing.T) {
	_, data := fixture(t, 50)
	c := newCache(t, 0, 0)
	remote := source.Content{MediaKind: "image", Kind: "https", URL: "https://images.example.com/original.png"}
	c.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Context().Err() != nil {
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), ContentLength: int64(len(data))}, nil
	})}
	result, err := c.Materialize(context.Background(), remote)
	if err != nil {
		t.Fatal(err)
	}
	if result.Width != 8 || result.Height != 4 {
		t.Fatal("did not inspect actual remote image")
	}
	exists(t, result.Path, true)
	c.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("")), ContentLength: MaxImageBytes + 1}, nil
	})
	if _, err := c.Materialize(context.Background(), remote); err == nil {
		t.Fatal("oversized remote accepted")
	}
	c.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	if _, err := c.Materialize(context.Background(), remote); err == nil {
		t.Fatal("HTTP error accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Materialize(ctx, remote); err == nil {
		t.Fatal("canceled operation accepted")
	}
}

func TestPreviewWhileOriginalDownloadIsStalled(t *testing.T) {
	cache := newCache(t, 2, 0)
	current, _ := fixture(t, 10)
	known, err := cache.Materialize(t.Context(), current)
	if err != nil {
		t.Fatal(err)
	}
	_, nextBytes := fixture(t, 20)
	started, resume := make(chan struct{}), make(chan struct{})
	cache.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		close(started)
		select {
		case <-resume:
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(nextBytes))}, nil
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	})}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := cache.Materialize(ctx, source.Content{
			MediaKind: "image", Kind: "https", URL: "https://images.example.com/next.png",
		}, known.Path)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the original download")
	}

	cold, err := cache.Preview(t.Context(), known)
	if err != nil {
		t.Fatalf("cold visible preview during original download: %v", err)
	}
	warm, err := cache.Preview(t.Context(), known)
	if err != nil || !bytes.Equal(warm.Data, cold.Data) {
		t.Fatalf("warm visible preview during original download: %v", err)
	}
	close(resume)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for original publication")
	}
	exists(t, known.Path, true)
}

func TestRecoveryAndCorruptContent(t *testing.T) {
	c := newCache(t, 0, 0)
	original, data := fixture(t, 1)
	result, err := c.Materialize(context.Background(), original)
	if err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(c.options.Dir, ".stage-abandoned")
	if err := os.WriteFile(stage, []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	c.Close()
	restarted, err := New(c.options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	exists(t, stage, false)
	exists(t, result.Path, true)
	if err := os.WriteFile(result.Path, bytes.Repeat([]byte("x"), len(data)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Materialize(context.Background(), original, result.Path); err == nil {
		t.Fatal("replaced protected corrupt content")
	}
	if _, err := restarted.Materialize(context.Background(), original); err != nil {
		t.Fatal(err)
	}
	fixed, err := os.ReadFile(result.Path)
	if err != nil || !bytes.Equal(data, fixed) {
		t.Fatal("did not reconcile corrupt copy")
	}
	if err := os.Remove(result.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Materialize(context.Background(), original); err != nil {
		t.Fatal(err)
	}
	exists(t, result.Path, true)
}

func TestReuseOfflineAndRefreshRecency(t *testing.T) {
	for _, format := range []string{"jpeg", "png", "webp"} {
		t.Run(format, func(t *testing.T) {
			c := newCache(t, 0, 0)
			original, _ := fixture(t, 71)
			switch format {
			case "jpeg":
				var data bytes.Buffer
				if err := jpeg.Encode(&data, image.NewRGBA(image.Rect(0, 0, 16, 8)), nil); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(original.Path, data.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
			case "webp":
				data, err := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4TBYAAAAvP8AIAAcQBSxg/wNAQvi/Xozof+oB")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(original.Path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			result, err := c.Materialize(t.Context(), original)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(original.Path); err != nil {
				t.Fatal(err)
			}
			old := time.Unix(1000, 0)
			if err := os.Chtimes(result.Path, old, old); err != nil {
				t.Fatal(err)
			}
			reused, err := c.Reuse(t.Context(), result, result.Path)
			if err != nil || reused != result || filepath.Ext(reused.Path) != imageExtension(format) {
				t.Fatalf("offline reuse: %+v %v", reused, err)
			}
			info, err := os.Stat(reused.Path)
			if err != nil || !info.ModTime().After(old) {
				t.Fatalf("reuse did not refresh recency: %v %v", info, err)
			}
		})
	}
}

func TestReuseRejectsInvalidRecords(t *testing.T) {
	c := newCache(t, 0, 0)
	original, data := fixture(t, 71)
	result, err := c.Materialize(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range []Result{
		{Path: original.Path, Hash: result.Hash, Width: result.Width, Height: result.Height, Bytes: result.Bytes},
		{Path: result.Path, Hash: result.Hash, Width: 0, Height: result.Height, Bytes: result.Bytes},
		{Path: result.Path, Hash: result.Hash, Width: result.Width, Height: result.Height, Bytes: result.Bytes + 1},
	} {
		if _, err := c.Reuse(t.Context(), record); err == nil {
			t.Fatalf("invalid cache record accepted: %+v", record)
		}
	}
	data[len(data)-1] ^= 1
	if err := os.WriteFile(result.Path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Reuse(t.Context(), result, result.Path); err == nil {
		t.Fatal("same-size replacement accepted")
	}
	if err := os.Remove(result.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Reuse(t.Context(), result); err == nil {
		t.Fatal("missing file accepted")
	}
}

func TestReuseEnforcesCapacityAndProtectsSelections(t *testing.T) {
	c := newCache(t, 3, 0)
	var records []Result
	for _, shade := range []uint8{1, 2, 3} {
		original, _ := fixture(t, shade)
		record, err := c.Materialize(t.Context(), original)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	current, pending, target := records[0], records[1], records[2]
	c.options.MaxImages = 2
	if _, err := c.Reuse(t.Context(), target, current.Path, pending.Path); !errors.Is(err, ErrCapacity) {
		t.Fatalf("reuse with protected selections: %v", err)
	}
	exists(t, pending.Path, true)
	if _, err := c.Reuse(t.Context(), target, current.Path); err != nil {
		t.Fatal(err)
	}
	exists(t, current.Path, true)
	exists(t, target.Path, true)
	exists(t, pending.Path, false)
}

func TestExclusiveOwnershipAndIdempotentClose(t *testing.T) {
	c := newCache(t, 0, 0)
	if other, err := New(c.options); err == nil {
		other.Close()
		t.Fatal("second cache owner accepted")
	}
	c.Close()
	c.Close()
	if _, err := c.Materialize(t.Context(), source.Content{MediaKind: "image", Kind: "local", Path: "/missing"}); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("closed cache accepted work: %v", err)
	}
	next, err := New(c.options)
	if err != nil {
		t.Fatalf("cache lock survived close: %v", err)
	}
	next.Close()
}

func TestHTTPSAddressAndRedirectPolicy(t *testing.T) {
	for _, value := range []string{"http://example.com/a.png", "https://127.0.0.1/a.png", "https://[::1]/a.png", "https://10.1.2.3/a.png", "https://169.254.169.254/a.png", "https://100.64.0.1/a.png", "https://example.com:8443/a.png", "https://user:secret@example.com/a.png", "https://example.com/a?token=secret"} {
		if err := validateURL(value); err == nil {
			t.Errorf("accepted %s", value)
		}
	}
	for _, value := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !publicAddress(netip.MustParseAddr(value)) {
			t.Errorf("rejected public %s", value)
		}
	}
	for _, value := range []string{"0.0.0.0", "192.168.0.1", "224.0.0.1", "192.0.2.1", "198.18.0.1", "::ffff:127.0.0.1", "fc00::1", "2001:db8::1", "2002:7f00:1::"} {
		if publicAddress(netip.MustParseAddr(value)) {
			t.Errorf("accepted nonpublic %s", value)
		}
	}
	client := secureClient()
	req, _ := http.NewRequest("GET", "https://127.0.0.1/private", nil)
	if err := client.CheckRedirect(req, []*http.Request{{}}); err == nil {
		t.Fatal("private redirect accepted")
	}
	req, _ = http.NewRequest("GET", "https://example.com/a", nil)
	if err := client.CheckRedirect(req, make([]*http.Request, 4)); err == nil {
		t.Fatal("redirect budget not enforced")
	}
	transport := client.Transport.(*http.Transport)
	if _, err := transport.DialContext(context.Background(), "tcp", net.JoinHostPort("localhost", "443")); err == nil {
		t.Fatal("private DNS accepted")
	}
}

func TestFullDecodeAllocationBound(t *testing.T) {
	// A real PNG header with dimensions beyond the limit is rejected before decode.
	_, data := fixture(t, 7)
	bad := append([]byte(nil), data...)
	binary.BigEndian.PutUint32(bad[16:20], 16_000_001)
	binary.BigEndian.PutUint32(bad[29:33], crc32.ChecksumIEEE(bad[12:29]))
	c := newCache(t, 0, 0)
	p := filepath.Join(t.TempDir(), "huge.png")
	if err := os.WriteFile(p, bad, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Materialize(context.Background(), source.Content{MediaKind: "image", Kind: "local", Path: p}); err == nil || !errors.As(err, new(*Error)) {
		t.Fatalf("image allocation limit not enforced: %v", err)
	}
}

func TestMaterializeRequiresExpectedRevisionBytes(t *testing.T) {
	c := newCache(t, 1, 0)
	reviewed, data := fixture(t, 10)
	hash := sha256.Sum256(data)
	reviewed.SHA256 = hex.EncodeToString(hash[:])
	first, err := c.Materialize(t.Context(), reviewed)
	if err != nil {
		t.Fatal(err)
	}
	replacement, _ := fixture(t, 20)
	replacement.SHA256 = reviewed.SHA256
	if _, err := c.Materialize(t.Context(), replacement); err == nil {
		t.Fatal("materialized unreviewed replacement")
	}
	entries, err := c.entries()
	if err != nil || len(entries) != 1 || entries[0].path != first.Path {
		t.Fatalf("rejected replacement changed cache: %v, %v", entries, err)
	}
}
