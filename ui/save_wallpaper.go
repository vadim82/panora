package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/playback"
)

type SaveWallpaperResult struct {
	Saved      bool   `json:"saved"`
	Already    bool   `json:"already_saved,omitempty"`
	FolderName string `json:"folder_name,omitempty"`
	Error      string `json:"error,omitempty"`
	Code       string `json:"code,omitempty"`
}

func (c *Client) SaveWallpaper(edit playback.SaveWallpaperRequest) SaveWallpaperResult {
	if !c.action.TryLock() {
		return SaveWallpaperResult{Error: "Another operation is running. Try again when it finishes.", Code: "busy"}
	}
	defer c.action.Unlock()
	ctx, cancel := context.WithTimeout(c.ctx, 15*time.Second)
	defer cancel()
	result, err := control.SaveWallpaper(ctx, c.socket, edit)
	if err != nil {
		return SaveWallpaperResult{Error: err.Error(), Code: requestErrorCode(err)}
	}
	if result.Version != playback.StatusVersion || !filepath.IsAbs(result.Path) {
		return SaveWallpaperResult{Error: "The service did not confirm where the wallpaper was saved."}
	}
	c.savedFolder = filepath.Dir(result.Path)
	return SaveWallpaperResult{Saved: true, Already: result.AlreadySaved, FolderName: filepath.Base(c.savedFolder)}
}

// The desktop opener takes only the folder from the last successful save. The
// frontend cannot pass an arbitrary filesystem location or URL.
func (c *Client) OpenSavedFolder() OpenResult {
	if !c.action.TryLock() {
		return OpenResult{Error: "Another operation is running. Try again when it finishes.", Code: "busy"}
	}
	folder := c.savedFolder
	c.action.Unlock()
	info, err := os.Stat(folder)
	if c.opener == nil || err != nil || !info.IsDir() {
		return OpenResult{Error: "The saved wallpaper folder is no longer available."}
	}
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	if err := c.opener(ctx, folder); err != nil {
		return OpenResult{Error: "The saved wallpaper folder could not be opened."}
	}
	return OpenResult{Opened: true}
}
