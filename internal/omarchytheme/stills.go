package omarchytheme

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/vadim82/panora/internal/localfiles"
)

const MaxStills = 512

type Still struct {
	Name string
	Path string
	Info os.FileInfo
}

func (s Still) Same(candidate Still) bool {
	return s.Path == candidate.Path && os.SameFile(s.Info, candidate.Info) &&
		s.Info.Size() == candidate.Info.Size() && s.Info.ModTime().Equal(candidate.Info.ModTime())
}

func (s Still) Available() bool {
	info, err := os.Stat(s.Path)
	return err == nil && s.Same(Still{Path: s.Path, Info: info})
}

// Stills selects the two one-level collections Omarchy uses. Nested intro and
// loop directories are outside this collection. Native file links are allowed.
func (a Active) Stills(ctx context.Context, home string) ([]Still, error) {
	var stills []Still
	seen := map[string]bool{}
	read := false
	var readErr error
	for _, root := range []string{
		filepath.Join(home, ".config", "omarchy", "backgrounds", a.ID),
		filepath.Join(a.Root, "backgrounds"),
	} {
		dir, err := os.Open(root)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				readErr = fmt.Errorf("some Omarchy backgrounds could not be read")
			}
			continue
		}
		info, err := dir.Stat()
		if err != nil || !info.IsDir() {
			dir.Close()
			readErr = fmt.Errorf("some Omarchy backgrounds could not be read")
			continue
		}
		read = true
		entries, err := dir.ReadDir(4097)
		dir.Close()
		if err != nil && !errors.Is(err, io.EOF) {
			readErr = fmt.Errorf("some Omarchy backgrounds could not be read")
		}
		if len(entries) > 4096 {
			return nil, fmt.Errorf("the active theme exceeds the background directory limit")
		}
		slices.SortFunc(entries, func(left, right os.DirEntry) int { return strings.Compare(left.Name(), right.Name()) })
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if entry.IsDir() || !localfiles.IsImage(entry.Name()) {
				continue
			}
			path, err := filepath.EvalSymlinks(filepath.Join(root, entry.Name()))
			if err != nil || seen[path] {
				continue
			}
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if len(stills) >= MaxStills {
				return nil, fmt.Errorf("the active theme exceeds the %d-still limit", MaxStills)
			}
			seen[path] = true
			stills = append(stills, Still{Name: entry.Name(), Path: path, Info: info})
		}
	}
	if !read && readErr == nil {
		readErr = fmt.Errorf("the active theme has no background collection")
	}
	return stills, readErr
}
