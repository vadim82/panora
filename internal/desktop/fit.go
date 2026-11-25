package desktop

import (
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// Bound full decoding and canvas allocation independently.
const maxFitPixels = 64_000_000

type fitTarget struct {
	Name   string
	Width  int
	Height int
	Scale  float64
}

func omarchyFitTarget(ctx context.Context) (fitTarget, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output, err := runBackendCommand(ctx, "hyprctl", "monitors", "-j")
	if err != nil {
		return fitTarget{}, fmt.Errorf("query Omarchy Fit monitor: %w", err)
	}
	return parseFitTarget([]byte(output))
}

func parseFitTarget(data []byte) (fitTarget, error) {
	geometry, err := parseHyprlandGeometry(data)
	if err != nil {
		return fitTarget{}, err
	}
	for _, monitor := range geometry.Displays {
		if monitor.Transform != 0 {
			return fitTarget{}, fmt.Errorf("Omarchy Fit currently requires an unrotated monitor; use --fit crop for native global behavior")
		}
	}
	if len(geometry.Displays) != 1 {
		return fitTarget{}, fmt.Errorf("Omarchy Fit requires exactly one active monitor (found %d); use --fit crop for native global behavior", len(geometry.Displays))
	}
	monitor := geometry.Displays[0]
	return fitTarget{Name: monitor.Name, Width: monitor.Width, Height: monitor.Height, Scale: monitor.Scale}, nil
}

func validateFitSize(width, height int) error {
	if width <= 0 || height <= 0 || width > maxFitPixels || height > maxFitPixels || int64(width)*int64(height) > maxFitPixels {
		return fmt.Errorf("dimensions %dx%d must be positive and at most %d pixels", width, height, maxFitPixels)
	}
	return nil
}

// fitRectangle rounds down so the complete image stays inside the canvas.
// Without enlargement, small originals keep their native pixel dimensions.
func fitRectangle(sourceWidth, sourceHeight, width, height int, upscale bool) image.Rectangle {
	w, h := sourceWidth, sourceHeight
	if upscale || w > width || h > height {
		if int64(width)*int64(h) <= int64(height)*int64(w) {
			h = max(1, int(int64(h)*int64(width)/int64(w)))
			w = width
		} else {
			w = max(1, int(int64(w)*int64(height)/int64(h)))
			h = height
		}
	}
	x, y := (width-w)/2, (height-h)/2
	return image.Rect(x, y, x+w, y+h)
}

func containedImage(ctx context.Context, source image.Image, width, height int, upscale bool) (*image.RGBA, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateFitSize(width, height); err != nil {
		return nil, err
	}
	bounds := source.Bounds()
	if err := validateFitSize(bounds.Dx(), bounds.Dy()); err != nil {
		return nil, err
	}
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
	rect := fitRectangle(bounds.Dx(), bounds.Dy(), width, height, upscale)
	// Over composites transparent PNGs onto the same opaque black canvas.
	draw.ApproxBiLinear.Scale(canvas, rect, source, bounds, draw.Over, nil)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return canvas, nil
}

func prepareContainedImage(ctx context.Context, path string, width, height int, upscale bool) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open Fit source: %w", err)
	}
	defer f.Close()
	config, format, err := image.DecodeConfig(f)
	if err != nil {
		return "", fmt.Errorf("decode Fit header: %w", err)
	}
	if format != "jpeg" && format != "png" && format != "webp" {
		return "", fmt.Errorf("Fit supports JPEG, PNG and static WebP, got %q", format)
	}
	if err := validateFitSize(config.Width, config.Height); err != nil {
		return "", fmt.Errorf("Fit source: %w", err)
	}
	if err := validateFitSize(width, height); err != nil {
		return "", fmt.Errorf("Fit canvas: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewind Fit source: %w", err)
	}
	source, _, err := image.Decode(f)
	if err != nil {
		return "", fmt.Errorf("decode full Fit image: %w", err)
	}
	if source.Bounds().Dx() != config.Width || source.Bounds().Dy() != config.Height {
		return "", fmt.Errorf("Fit image dimensions changed during decoding")
	}
	canvas, err := containedImage(ctx, source, width, height, upscale)
	if err != nil {
		return "", err
	}
	dir, err := fitCacheDirectory()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create Fit cache: %w", err)
	}
	// Publish only a complete PNG. The content name reuses identical results and
	// never overwrites originals. Keep it after exit: Omarchy's link references it.
	out, err := os.CreateTemp(dir, ".fit-*.png")
	if err != nil {
		return "", fmt.Errorf("create Fit image: %w", err)
	}
	defer os.Remove(out.Name())
	defer out.Close()
	hash := sha256.New()
	encoder := png.Encoder{CompressionLevel: png.NoCompression}
	if err := encoder.Encode(io.MultiWriter(out, hash), canvas); err != nil {
		return "", fmt.Errorf("encode Fit image: %w", err)
	}
	if err := out.Close(); err != nil {
		return "", fmt.Errorf("close Fit image: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	result := filepath.Join(dir, fmt.Sprintf("%x.png", hash.Sum(nil)))
	if err := os.Rename(out.Name(), result); err != nil {
		return "", fmt.Errorf("publish Fit image: %w", err)
	}
	return result, nil
}
