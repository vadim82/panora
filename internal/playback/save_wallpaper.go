package playback

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/vadim82/panora/internal/cache"
)

type SaveWallpaperRequest struct {
	Instance string `json:"instance"`
	History  string `json:"history"`
	ImageID  int64  `json:"image_id"`
	Revision string `json:"revision"`
	Folder   string `json:"folder,omitempty"`
}

type SavedWallpaper struct {
	Version      int    `json:"version"`
	Path         string `json:"path"`
	AlreadySaved bool   `json:"already_saved"`
}

// SaveWallpaper holds the action lock until the copy is published. It does
// not enter the playback action path or change rotation or history state.
func (s *Service) SaveWallpaper(ctx context.Context, request SaveWallpaperRequest) (SavedWallpaper, error) {
	if request.Instance == "" || len(request.Instance) > 128 || !ValidHistoryReference(request.History) ||
		request.ImageID <= 0 || len(request.Revision) != 64 || request.Folder != "" && !validFolderPath(request.Folder) {
		return SavedWallpaper{}, &Error{Code: "invalid_request", Message: "provide a service instance, accepted image, and reviewed settings revision"}
	}
	if !s.actionMu.TryLock() {
		return SavedWallpaper{}, &Error{Code: "busy", Message: "Wallpaper playback is busy. Try again when it finishes."}
	}
	defer s.actionMu.Unlock()
	if request.Instance != s.instanceID {
		return SavedWallpaper{}, &Error{Code: "instance_changed", Message: "The wallpaper service changed. Review the current wallpaper and try again."}
	}
	if s.state.Cursor < 0 {
		return SavedWallpaper{}, changedSaveWallpaper()
	}
	entry := s.state.History[s.state.Cursor]
	if entry.ID != request.History || entry.Identity.ImageID != request.ImageID || s.state.Pending != nil {
		return SavedWallpaper{}, changedSaveWallpaper()
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	defer cancel()
	config, _, data, err := ReadConfig(s.request.ConfigPath)
	if err != nil {
		return SavedWallpaper{}, invalidSavedConfiguration()
	}
	if request.Revision != configRevision(data) {
		return SavedWallpaper{}, &Error{Code: "config_conflict", Message: "Settings changed. Review the save folder and try again."}
	}
	folder := request.Folder
	if folder == "" {
		folder = config.SaveFolder
	}
	if folder == "" {
		return SavedWallpaper{}, &Error{Code: "save_folder", Message: "Choose a folder for saved wallpapers."}
	}
	known, ok, err := s.store.KnownCachedImage(ctx, request.ImageID)
	if ctx.Err() != nil {
		return SavedWallpaper{}, saveWallpaperTimeout()
	}
	if err != nil || !ok || known.Path != entry.Path || known.Hash != s.currentHash {
		return SavedWallpaper{}, unavailableSaveWallpaper()
	}
	path, alreadySaved, err := s.cache.Export(ctx, cache.Result(known), filepath.Clean(folder))
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return SavedWallpaper{}, saveWallpaperTimeout()
	case errors.Is(err, cache.ErrPreviewBusy):
		return SavedWallpaper{}, &Error{Code: "busy", Message: "The wallpaper cache is busy. Try again shortly."}
	case errors.Is(err, cache.ErrExportFolder):
		return SavedWallpaper{}, &Error{Code: "save_folder", Message: "Panora could not save to this folder. Choose another folder."}
	case err != nil:
		return SavedWallpaper{}, unavailableSaveWallpaper()
	default:
		return SavedWallpaper{Version: StatusVersion, Path: path, AlreadySaved: alreadySaved}, nil
	}
}

func changedSaveWallpaper() *Error {
	return &Error{Code: "current_changed", Message: "The wallpaper changed. Review the current wallpaper and try again."}
}

func unavailableSaveWallpaper() *Error {
	return &Error{Code: "image_unavailable", Message: "The validated full-resolution wallpaper is no longer available."}
}

func saveWallpaperTimeout() *Error {
	return &Error{Code: "save_timeout", Message: "Saving was interrupted. Try again to check whether the wallpaper was saved."}
}
