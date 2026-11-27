package control

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/vadim82/panora/internal/playback"
)

const saveWallpaperEndpoint = "/v1/wallpapers/save"

type saveWallpaperService interface {
	SaveWallpaper(context.Context, playback.SaveWallpaperRequest) (playback.SavedWallpaper, error)
}

func handleSaveWallpaper(ctx context.Context, w http.ResponseWriter, r *http.Request, service Service) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST to save the accepted wallpaper")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 32769))
	if err != nil || len(body) > 32768 {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "invalid_request", "wallpaper save request exceeds 32 KiB")
		return
	}
	var edit playback.SaveWallpaperRequest
	if r.URL.RawQuery != "" || decodeConfigurationEdit(body, &edit) != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "provide one wallpaper save request object")
		return
	}
	save, ok := service.(saveWallpaperService)
	if !ok {
		writeServiceError(w, &Error{Code: "unsupported", Message: "wallpaper saving is unavailable"})
		return
	}
	result, err := save.SaveWallpaper(ctx, edit)
	writeResult(w, result, err)
}

func SaveWallpaper(ctx context.Context, path string, edit playback.SaveWallpaperRequest) (playback.SavedWallpaper, error) {
	body, err := json.Marshal(edit)
	if err != nil {
		return playback.SavedWallpaper{}, err
	}
	var result playback.SavedWallpaper
	err = requestBody(ctx, path, http.MethodPost, saveWallpaperEndpoint, body, &result, 32768)
	return result, err
}
