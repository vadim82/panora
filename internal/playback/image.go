package playback

import (
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math/bits"

	"github.com/vadim82/panora/internal/cache"
	"github.com/vadim82/panora/internal/localfiles"
)

var errImageHeaderBudget = errors.New("image exceeds the header inspection byte allowance")

type ImageInfo struct {
	Path   string
	Format string
	Width  int
	Height int
}

func (i ImageInfo) String() string {
	return fmt.Sprintf("[%d, %d] %s", i.Width, i.Height, i.Path)
}

func ReadImageInfo(path string) (ImageInfo, error) {
	return readImageInfoContext(context.Background(), path)
}

func readImageInfoContext(ctx context.Context, path string) (ImageInfo, error) {
	return readImageInfoLimited(ctx, path, cache.MaxImageBytes)
}

// Validate the opened descriptor and cap reads by the caller's reserved bytes.
// A pathname stat made before opening cannot authorize a larger replacement.
func readImageInfoLimited(ctx context.Context, path string, maxBytes int64) (ImageInfo, error) {
	return readImageHeader(ctx, path, maxBytes, maxBytes)
}

func readImageHeader(ctx context.Context, path string, maxBytes, maxHeaderBytes int64) (ImageInfo, error) {
	if err := ctx.Err(); err != nil {
		return ImageInfo{}, err
	}
	f, err := localfiles.OpenRegular(path)
	if err != nil {
		return ImageInfo{}, fmt.Errorf("open %q: %w", path, err)
	}

	defer func() {
		_ = f.Close()
	}()
	info, err := f.Stat()
	if err != nil {
		return ImageInfo{}, err
	}
	if maxBytes < 0 || info.Size() > maxBytes {
		return ImageInfo{}, errImageHeaderBudget
	}
	config, format, err := image.DecodeConfig(io.LimitReader(imageHeaderReader{ctx: ctx, reader: f}, min(maxBytes, maxHeaderBytes)))
	if err != nil {
		return ImageInfo{}, fmt.Errorf("decode file %q: %w", path, err)
	}

	return ImageInfo{
		Path:   path,
		Format: format,
		Width:  config.Width,
		Height: config.Height,
	}, nil
}

func (i ImageInfo) meetsMinResolution(minWidth, minHeight int) bool {
	return i.Width >= minWidth && i.Height >= minHeight
}

func (i ImageInfo) matchesRatio(ratio AspectRatio, similar bool, tolerancePixels int) bool {
	return i.matchesRatioTolerance(ratio, similar, tolerancePixels, 5)
}

func (i ImageInfo) matchesRatioTolerance(ratio AspectRatio, similar bool, tolerancePixels, percent int) bool {
	if i.Width <= 0 || i.Height <= 0 || ratio.Width <= 0 || ratio.Height <= 0 {
		return false
	}

	// Positive int factors fit uint64; their products fit 128 bits. Subtract
	// the smaller product from the larger so no signed product, subtraction,
	// or absolute-value operation can wrap, even at the platform's max int.
	leftHi, leftLo := bits.Mul64(uint64(i.Width), uint64(ratio.Height))
	rightHi, rightLo := bits.Mul64(uint64(i.Height), uint64(ratio.Width))
	expectedHi, expectedLo := rightHi, rightLo
	if leftHi < rightHi || leftHi == rightHi && leftLo < rightLo {
		leftHi, rightHi = rightHi, leftHi
		leftLo, rightLo = rightLo, leftLo
	}
	differenceLo, borrow := bits.Sub64(leftLo, rightLo, 0)
	differenceHi, _ := bits.Sub64(leftHi, rightHi, borrow)

	if similar {
		// Divide before multiplying to keep the 128-bit intermediate bounded.
		const denominator = 100
		qHi := expectedHi / denominator
		qLo, rem := bits.Div64(expectedHi%denominator, expectedLo, denominator)
		carry, toleranceLo := bits.Mul64(qLo, uint64(percent))
		toleranceHi := qHi*uint64(percent) + carry
		toleranceLo, carry = bits.Add64(toleranceLo, rem*uint64(percent)/denominator, 0)
		toleranceHi += carry
		return differenceHi < toleranceHi || differenceHi == toleranceHi && differenceLo <= toleranceLo
	}

	// Allow either dimension to differ from the ideal ratio by the configured pixel count.
	// This does not relax the separate minimum-resolution check.
	toleranceHi, toleranceLo := bits.Mul64(uint64(max(ratio.Width, ratio.Height)), uint64(tolerancePixels))
	return differenceHi < toleranceHi || differenceHi == toleranceHi && differenceLo <= toleranceLo
}

type AspectRatio struct {
	Width  int
	Height int
}

// Context checks bound cooperative reads; the OS may still block an individual read.
type imageHeaderReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r imageHeaderReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
