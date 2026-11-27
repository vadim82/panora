package main

import (
	"context"
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
	return SaveWallpaperResult{Saved: true, Already: result.AlreadySaved, FolderName: filepath.Base(filepath.Dir(result.Path))}
}
