package cache

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

type Limits struct {
	MaxImages int   `json:"max_images"`
	MaxBytes  int64 `json:"max_bytes"`
}

// Usage counts managed regular cache files once, independently of the number of
// catalog images sharing them. Unknown state means the counters are not usable.
type Usage struct {
	State           string `json:"state"`
	Images          int    `json:"images"`
	Bytes           int64  `json:"bytes"`
	ProtectedImages int    `json:"protected_images"`
	ProtectedBytes  int64  `json:"protected_bytes"`
}

type Snapshot struct {
	Limits Limits
	Usage  Usage
}

func (c *Cache) limits() Limits {
	return Limits{MaxImages: c.options.MaxImages, MaxBytes: c.options.MaxBytes}
}

// Usage is read-only and serializes with publication, eviction and preview reads.
func (c *Cache) Usage(ctx context.Context, protected ...string) (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	snapshot := Snapshot{Limits: c.limits(), Usage: Usage{State: "unknown"}}
	if err := ctx.Err(); err != nil {
		return snapshot, err
	}
	if c.release == nil {
		return snapshot, fmt.Errorf("playback cache is closed")
	}
	entries, err := c.entries()
	if err != nil {
		return snapshot, err
	}
	if err := ctx.Err(); err != nil {
		return snapshot, err
	}
	snapshot.Usage, err = usage(entries, snapshot.Limits, protected)
	return snapshot, err
}

// UpdateLimits activates validated budgets before cleanup. Protected files can
// exceed the budget; they are retained and reported instead of being deleted.
// A later cleanup error leaves the new limits active and any completed evictions
// intact. Callers persist settings before invoking this operation.
func (c *Cache) UpdateLimits(ctx context.Context, maxImages int, maxBytes int64, protected ...string) (Snapshot, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	snapshot := Snapshot{Limits: c.limits(), Usage: Usage{State: "unknown"}}
	if maxImages < 1 || maxBytes < 1 {
		return snapshot, fmt.Errorf("cache limits must be positive")
	}
	if err := ctx.Err(); err != nil {
		return snapshot, err
	}
	if c.release == nil {
		return snapshot, fmt.Errorf("playback cache is closed")
	}
	c.options.MaxImages, c.options.MaxBytes = maxImages, maxBytes
	snapshot.Limits = c.limits()
	entries, err := c.entries()
	if err != nil {
		return snapshot, err
	}
	if err := ctx.Err(); err != nil {
		return snapshot, err
	}
	snapshot.Usage, err = usage(entries, snapshot.Limits, protected)
	if err != nil {
		return snapshot, err
	}
	keep := protectedPaths(protected)
	for _, entry := range entries {
		if snapshot.Usage.Images <= maxImages && snapshot.Usage.Bytes <= maxBytes {
			break
		}
		if keep[entry.path] {
			continue
		}
		if err := ctx.Err(); err != nil {
			return snapshot, err
		}
		if err := os.Remove(entry.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return snapshot, fmt.Errorf("evict cached image: %w", err)
		}
		snapshot.Usage.Images--
		snapshot.Usage.Bytes -= entry.size
	}
	setUsageState(&snapshot.Usage, snapshot.Limits)
	return snapshot, nil
}

func protectedPaths(paths []string) map[string]bool {
	keep := make(map[string]bool, len(paths))
	for _, path := range paths {
		if path != "" {
			keep[filepath.Clean(path)] = true
		}
	}
	return keep
}

func usage(entries []diskEntry, limits Limits, protected []string) (Usage, error) {
	result := Usage{}
	keep := protectedPaths(protected)
	for _, entry := range entries {
		if entry.size < 0 || entry.size > math.MaxInt64-result.Bytes {
			return Usage{State: "unknown"}, fmt.Errorf("cache usage exceeds numeric range")
		}
		result.Images++
		result.Bytes += entry.size
		if keep[entry.path] {
			result.ProtectedImages++
			result.ProtectedBytes += entry.size
		}
	}
	setUsageState(&result, limits)
	return result, nil
}

func setUsageState(result *Usage, limits Limits) {
	result.State = "within_budget"
	if result.Images > limits.MaxImages || result.Bytes > limits.MaxBytes {
		result.State = "over_budget"
	}
	if result.ProtectedImages > limits.MaxImages || result.ProtectedBytes > limits.MaxBytes {
		result.State = "protected_over_budget"
	}
}
