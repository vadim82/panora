package cache

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/vadim82/panora/internal/localfiles"
)

var ErrExportFolder = errors.New("cannot write to the save folder")

// Export copies the validated full-size bytes into a file outside cache
// ownership. The complete file is published under a new name without replacing
// anything, even when the destination is used by concurrent clients.
func (c *Cache) Export(ctx context.Context, known Result, folder string) (path string, alreadySaved bool, err error) {
	if !c.mu.TryLock() {
		return "", false, ErrPreviewBusy
	}
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	source, digest, err := c.openKnownImage(known)
	if err != nil {
		return "", false, err
	}
	defer source.Close()

	targetPath, err := filepath.EvalSymlinks(folder)
	if err != nil {
		return "", false, ErrExportFolder
	}
	cachePath, err := filepath.EvalSymlinks(c.options.Dir)
	if err != nil {
		return "", false, ErrExportFolder
	}
	if relative, err := filepath.Rel(cachePath, targetPath); err != nil || filepath.IsLocal(relative) {
		return "", false, ErrExportFolder
	}
	target, err := os.OpenRoot(targetPath)
	if err != nil {
		return "", false, ErrExportFolder
	}
	defer target.Close()
	name, exists := exportName(ctx, target, known, digest)
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if name == "" {
		return "", false, ErrExportFolder
	}
	if exists {
		if err := verifyKnownImage(ctx, source, known.Bytes, digest); err != nil {
			return "", false, err
		}
		return filepath.Join(targetPath, name), true, nil
	}
	temporary := ".panora-save-" + rand.Text()
	staged, err := target.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", false, ErrExportFolder
	}
	defer target.Remove(temporary)
	defer staged.Close()

	var writeFailed bool
	if err := verifyKnownImage(ctx, io.TeeReader(source, exportWriter{staged, &writeFailed}), known.Bytes, digest); err != nil {
		if writeFailed && ctx.Err() == nil {
			return "", false, ErrExportFolder
		}
		return "", false, err
	}
	if err := staged.Sync(); err != nil {
		return "", false, ErrExportFolder
	}
	if err := staged.Close(); err != nil {
		return "", false, ErrExportFolder
	}
	for attempt := 0; attempt < 32; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", false, err
		}
		err := target.Link(temporary, name)
		if err == nil {
			return filepath.Join(targetPath, name), false, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", false, ErrExportFolder
		}
		name, exists = exportName(ctx, target, known, digest)
		if err := ctx.Err(); err != nil {
			return "", false, err
		}
		if name == "" {
			return "", false, ErrExportFolder
		}
		if exists {
			return filepath.Join(targetPath, name), true, nil
		}
	}
	return "", false, ErrExportFolder
}

func exportName(ctx context.Context, folder *os.Root, image Result, digest []byte) (name string, exists bool) {
	stem := "Panora-" + image.Hash
	ext := filepath.Ext(image.Path)
	var vacant string
	for i := 0; i < 32 && ctx.Err() == nil; i++ {
		candidate := stem + ext
		if i > 0 {
			candidate = stem + "-" + strconv.Itoa(i+1) + ext
		}
		info, err := folder.Lstat(candidate)
		if errors.Is(err, os.ErrNotExist) && vacant == "" {
			vacant = candidate
		}
		if err == nil && info.Mode().IsRegular() && info.Size() == image.Bytes {
			// This shared opener rejects symlinks and uses nonblocking open,
			// including if a special file replaces the regular entry.
			if file, err := localfiles.OpenRegular(filepath.Join(folder.Name(), candidate)); err == nil {
				actual, statErr := file.Stat()
				match := statErr == nil && os.SameFile(info, actual) && verifyKnownImage(ctx, file, image.Bytes, digest) == nil
				file.Close()
				if match {
					return candidate, true
				}
			}
		}
	}
	return vacant, false
}

// Wrap write failures separately from unavailable source bytes.
type exportWriter struct {
	io.Writer
	failed *bool
}

func (writer exportWriter) Write(data []byte) (int, error) {
	count, err := writer.Writer.Write(data)
	if err != nil {
		*writer.failed = true
	}
	return count, err
}
