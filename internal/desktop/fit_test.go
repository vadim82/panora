package desktop

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFitGeometry(t *testing.T) {
	for _, tt := range []struct {
		name         string
		sw, sh, w, h int
		upscale      bool
		want         image.Rectangle
	}{
		{"wide letterbox", 16, 4, 8, 8, false, image.Rect(0, 3, 8, 5)},
		{"portrait pillarbox", 4, 16, 8, 8, false, image.Rect(3, 0, 5, 8)},
		{"same ratio", 16, 8, 8, 4, false, image.Rect(0, 0, 8, 4)},
		{"small image stays small", 2, 2, 8, 4, false, image.Rect(3, 1, 5, 3)},
		{"explicit enlargement", 2, 2, 8, 4, true, image.Rect(2, 0, 6, 4)},
		{"odd padding", 3, 3, 8, 6, false, image.Rect(2, 1, 5, 4)},
		{"one pixel thin", 1000, 1, 8, 4, false, image.Rect(0, 1, 8, 2)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := fitRectangle(tt.sw, tt.sh, tt.w, tt.h, tt.upscale); got != tt.want {
				t.Fatalf("rectangle = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainedImagePreservesEdgesAndCompositesTransparency(t *testing.T) {
	// Nonzero bounds exercise image origins independently of decoder behavior.
	source := image.NewNRGBA(image.Rect(5, 6, 7, 8))
	source.SetNRGBA(5, 6, color.NRGBA{R: 255, A: 255})
	source.SetNRGBA(6, 6, color.NRGBA{G: 255, A: 255})
	source.SetNRGBA(5, 7, color.NRGBA{B: 255, A: 255})
	source.SetNRGBA(6, 7, color.NRGBA{R: 200, A: 128})
	got, err := containedImage(context.Background(), source, 6, 4, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		x, y int
		want color.RGBA
	}{
		{0, 0, color.RGBA{A: 255}},
		{5, 3, color.RGBA{A: 255}},
		{2, 1, color.RGBA{R: 255, A: 255}},
		{3, 1, color.RGBA{G: 255, A: 255}},
		{2, 2, color.RGBA{B: 255, A: 255}},
		{3, 2, color.RGBA{R: 100, A: 255}},
	} {
		if pixel := got.RGBAAt(tt.x, tt.y); pixel != tt.want {
			t.Fatalf("pixel (%d,%d) = %v, want %v", tt.x, tt.y, pixel, tt.want)
		}
	}
}

func TestParseFitTarget(t *testing.T) {
	for _, tt := range []struct {
		name, data, wantError string
	}{
		{"valid", `[{"name":"DP-2","width":5120,"height":1440,"scale":1,"transform":0}]`, ""},
		{"disabled ignored", `[{"disabled":true},{"name":"DP-2","width":5120,"height":1440,"scale":1}]`, ""},
		{"none", `[]`, "exactly one"},
		{"multiple", `[{"name":"A","width":1,"height":1,"scale":1},{"name":"B","width":1,"height":1,"scale":1}]`, "exactly one"},
		{"rotated", `[{"name":"DP-2","width":5120,"height":1440,"scale":1,"transform":1}]`, "unrotated"},
		{"bad dimensions", `[{"name":"DP-2","width":0,"height":1440,"scale":1}]`, "dimensions"},
		{"oversized", `[{"name":"DP-2","width":10000,"height":10000,"scale":1}]`, "dimensions"},
		{"missing scale", `[{"name":"DP-2","width":5120,"height":1440}]`, "scale"},
		{"missing name", `[{"width":5120,"height":1440,"scale":1}]`, "name"},
		{"not JSON", `Hyprland unavailable`, "decode"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseFitTarget([]byte(tt.data))
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("got %v, want %q", err, tt.wantError)
				}
			} else if err != nil || got != (fitTarget{"DP-2", 5120, 1440, 1}) {
				t.Fatalf("target = %+v, error = %v", got, err)
			}
		})
	}
}

func TestPrepareContainedImageCacheAndOriginal(t *testing.T) {
	setTestCache(t, filepath.Join(t.TempDir(), "cache $(literal)"))
	path := filepath.Join(t.TempDir(), "source.png")
	writeTestImage(t, path, 2, 1)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	first, err := prepareContainedImage(context.Background(), path, 8, 4, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := prepareContainedImage(context.Background(), path, 8, 4, false)
	if err != nil || first != second || !filepath.IsAbs(first) || first == path {
		t.Fatalf("paths = %q, %q, error = %v", first, second, err)
	}
	f, err := os.Open(first)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil || img.Bounds() != image.Rect(0, 0, 8, 4) {
		t.Fatalf("cached image = %v, error = %v", img, err)
	}
	entries, err := os.ReadDir(filepath.Dir(first))
	if err != nil || len(entries) != 1 {
		t.Fatalf("cache entries = %v, error = %v", entries, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatalf("original changed, error = %v", err)
	}
}

func TestFitEncodingPreservesDecodedPixels(t *testing.T) {
	encodeJPEG := func(w io.Writer, img image.Image) error {
		return jpeg.Encode(w, img, nil)
	}
	setTestCache(t, t.TempDir())
	fixture := image.NewNRGBA(image.Rect(0, 0, 12, 6))
	alpha := []uint8{0, 128, 255}
	for y := range fixture.Rect.Dy() {
		for x := range fixture.Rect.Dx() {
			fixture.SetNRGBA(x, y, color.NRGBA{R: uint8(15 * x), G: uint8(30 * y), B: 220, A: alpha[x%len(alpha)]})
		}
	}

	for _, tt := range []struct {
		name          string
		encode        func(io.Writer, image.Image) error
		width, height int
		upscale       bool
	}{
		{"JPEG downscale and letterbox", encodeJPEG, 8, 8, false},
		{"PNG alpha and padding", png.Encode, 20, 20, false},
		{"PNG alpha and enlargement", png.Encode, 20, 20, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var input bytes.Buffer
			if err := tt.encode(&input, fixture); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "source")
			if err := os.WriteFile(path, input.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}

			source, _, err := image.Decode(&input)
			if err != nil {
				t.Fatal(err)
			}
			want, err := containedImage(context.Background(), source, tt.width, tt.height, tt.upscale)
			if err != nil {
				t.Fatal(err)
			}

			result, err := prepareContainedImage(context.Background(), path, tt.width, tt.height, tt.upscale)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(result)
			if err != nil {
				t.Fatal(err)
			}
			if name := fmt.Sprintf("%x.png", sha256.Sum256(data)); filepath.Base(result) != name {
				t.Fatalf("published name = %q, want %q", filepath.Base(result), name)
			}
			got, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if got.Bounds() != want.Bounds() {
				t.Fatalf("bounds = %v, want %v", got.Bounds(), want.Bounds())
			}
			pixels := image.NewRGBA(got.Bounds())
			draw.Draw(pixels, pixels.Bounds(), got, got.Bounds().Min, draw.Src)
			if !bytes.Equal(pixels.Pix, want.Pix) {
				t.Fatal("decoded pixels differ from the Fit canvas")
			}
		})
	}
}

func TestFitRejectsPartialImageAndCancellation(t *testing.T) {
	setTestCache(t, t.TempDir())
	path := filepath.Join(t.TempDir(), "truncated.png")
	writeTestImage(t, path, 2, 2)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data[:33], 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(data[:33])); err != nil {
		t.Fatalf("fixture must have a valid header: %v", err)
	}
	if _, err := prepareContainedImage(context.Background(), path, 8, 4, false); err == nil {
		t.Fatal("partial image accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := prepareContainedImage(ctx, path, 8, 4, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want cancellation", err)
	}
}

func TestFitRejectsOversizedSourceBeforeDecode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.png")
	writeTestImage(t, path, 2, 2)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Keep a valid IHDR CRC so header inspection reports the oversized dimensions.
	binary.BigEndian.PutUint32(data[16:20], 10000)
	binary.BigEndian.PutUint32(data[20:24], 10000)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareContainedImage(context.Background(), path, 8, 4, false); err == nil || !strings.Contains(err.Error(), "Fit source: dimensions") {
		t.Fatalf("got %v, want source pixel-limit error before full decoding", err)
	}
}

func TestFitJPEGAndFailedPublicationCleanup(t *testing.T) {
	setTestCache(t, t.TempDir())
	path := filepath.Join(t.TempDir(), "source.jpg")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	err = jpeg.Encode(f, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("encode JPEG: %v; close: %v", err, closeErr)
	}
	result, err := prepareContainedImage(context.Background(), path, 8, 4, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(result); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(result, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareContainedImage(context.Background(), path, 8, 4, false); err == nil || !strings.Contains(err.Error(), "publish Fit image") {
		t.Fatalf("got %v, want publication error", err)
	}
	entries, err := os.ReadDir(filepath.Dir(result))
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("partial image left behind: entries=%v, error=%v", entries, err)
	}
}
