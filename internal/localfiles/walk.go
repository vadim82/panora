// Package localfiles shares directory traversal and path containment checks.
package localfiles

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func IsImage(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".png", ".webp":
		return true
	}
	return false
}

// Walk visits JPEG/PNG/WebP paths in directory traversal order. Root files are depth
// zero. A configured root may be a symlink, but links beneath it are skipped.
func Walk(ctx context.Context, root string, maxDepth int, visit func(string) error) error {
	if maxDepth < 0 {
		return fmt.Errorf("max depth must be non-negative")
	}
	walkRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("walk %q: %w", root, err)
	}
	return filepath.WalkDir(walkRoot, func(path string, entry fs.DirEntry, err error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err != nil {
			return fmt.Errorf("walk %q: %w", path, err)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			relative, err := filepath.Rel(walkRoot, path)
			if err != nil {
				return err
			}
			if relative != "." && strings.Count(relative, string(filepath.Separator))+1 > maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() || !IsImage(path) {
			return nil
		}
		relative, err := filepath.Rel(walkRoot, path)
		if err != nil {
			return err
		}
		return visit(filepath.Join(root, relative))
	})
}

// ContainedPath checks both lexical scope/depth and the current symlink target.
// The filesystem may still change after this check; it is not a sandbox.
func ContainedPath(root, path string, maxDepth int) (string, error) {
	if !filepath.IsAbs(root) || !filepath.IsAbs(path) {
		return "", fmt.Errorf("local content paths must be absolute")
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || strings.Count(relative, string(filepath.Separator)) > maxDepth {
		return "", fmt.Errorf("local content is outside the source or scan depth")
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	relative, err = filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("local content symlink escapes the source")
	}
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || !IsImage(path) {
		return "", fmt.Errorf("local content must be a regular JPEG, PNG, or static WebP file")
	}
	return filepath.Clean(path), nil
}
