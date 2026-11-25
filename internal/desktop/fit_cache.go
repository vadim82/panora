package desktop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

const (
	fitCacheMaxFiles = 20
	fitCacheMaxBytes = int64(512 << 20)
)

var fitCacheName = regexp.MustCompile(`^[0-9a-f]{64}\.png$`)
var fitTempName = regexp.MustCompile(`^\.fit-[0-9]+\.png$`)

func fitCacheDirectory() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("find Fit cache: %w", err)
	}
	if !filepath.IsAbs(cache) {
		return "", fmt.Errorf("Fit cache must have an absolute path; check XDG_CACHE_HOME")
	}
	return filepath.Join(cache, "panora", "rendered"), nil
}

// The installed native setter uses this HOME-relative link, independently of
// XDG_STATE_HOME. A missing link is a fresh desktop; an unreadable or dangling
// existing link makes deletion unsafe, so cleanup must fail before application.
func currentOmarchyBackground() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	link := filepath.Join(home, ".local", "state", "omarchy", "current", "background")
	if _, err := os.Lstat(link); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(link)
}

type fitCacheEntry struct {
	path      string
	size      int64
	modified  time.Time
	temporary bool
}

// Caller holds the cache process lock through preparation and application.
// Only regular Panora-named artifacts are eligible; originals, symlinks,
// directories, unknown files, and protected paths are never deleted.
func pruneFitCache(ctx context.Context, dir string, maxFiles int, maxBytes int64, protected ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return fmt.Errorf("resolve Fit cache: %w", err)
	}
	keep := make(map[string]bool)
	for _, path := range protected {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			keep[resolved] = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("protect Fit image: %w", err)
		}
	}
	protectCurrent := func() error {
		path, err := currentOmarchyBackground()
		if err != nil {
			return fmt.Errorf("protect current Omarchy wallpaper: %w", err)
		}
		if path != "" {
			keep[path] = true
		}
		return nil
	}
	if err := protectCurrent(); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read Fit cache: %w", err)
	}
	var owned []fitCacheEntry
	var bytes int64
	for _, entry := range entries {
		temporary := fitTempName.MatchString(entry.Name())
		if !temporary && !fitCacheName.MatchString(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		owned = append(owned, fitCacheEntry{filepath.Join(dir, entry.Name()), info.Size(), info.ModTime(), temporary})
		bytes += info.Size()
	}
	sort.Slice(owned, func(i, j int) bool {
		if owned[i].temporary != owned[j].temporary {
			return owned[i].temporary
		}
		if owned[i].modified.Equal(owned[j].modified) {
			return owned[i].path < owned[j].path
		}
		return owned[i].modified.Before(owned[j].modified)
	})
	count := len(owned)
	for _, entry := range owned {
		if !entry.temporary && count <= maxFiles && bytes <= maxBytes {
			break
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// Native theme/background changes do not take Panora's lock. Refresh
		// the link before each removal and retain every observed current file.
		if err := protectCurrent(); err != nil {
			return err
		}
		if keep[entry.path] {
			continue
		}
		if err := os.Remove(entry.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove unused Fit image: %w", err)
		}
		count--
		bytes -= entry.size
	}
	if count > maxFiles || bytes > maxBytes {
		return fmt.Errorf("Fit cache cannot retain protected images within %d files and %d bytes", maxFiles, maxBytes)
	}
	return nil
}
