package playback

import (
	"context"
	"fmt"

	"github.com/vadim82/panora/internal/localfiles"
)

type ScanResult struct {
	Image   ImageInfo
	Reasons []string
	Err     error // A per-file read/decode error; Image.Path is still available.
}

func inspectImageContext(ctx context.Context, path string, minWidth, minHeight int, ratio *RatioRule) ScanResult {
	image, err := readImageInfoContext(ctx, path)
	if err != nil {
		return ScanResult{Image: ImageInfo{Path: path}, Err: err}
	}
	result := ScanResult{Image: image}
	if !image.meetsMinResolution(minWidth, minHeight) {
		result.Reasons = append(result.Reasons, fmt.Sprintf("below minimum %dx%d", minWidth, minHeight))
	}
	if !ratio.matches(image) {
		result.Reasons = append(result.Reasons, "does not match ratio "+ratio.label)
	}
	return result
}

// ScanDirectoryContext streams read-only header diagnostics with the same ratio
// matcher as playback and history. Providers own playback's candidate listing.
func ScanDirectoryContext(ctx context.Context, root string, maxDepth, minWidth, minHeight int, ratio *RatioRule, visit func(ScanResult) error) error {
	if maxDepth < 0 {
		return fmt.Errorf("max depth must be non-negative")
	}
	if minWidth <= 0 || minHeight <= 0 {
		return fmt.Errorf("minimum width and height must be positive")
	}
	if visit == nil {
		return fmt.Errorf("scan callback must not be nil")
	}
	return localfiles.Walk(ctx, root, maxDepth, func(path string) error {
		if err := visit(inspectImageContext(ctx, path, minWidth, minHeight, ratio)); err != nil {
			return fmt.Errorf("handle scan result for %q: %w", path, err)
		}
		return nil
	})
}
