package control

import (
	"context"
	"io"
	"net/http"

	"github.com/vadim82/panora/internal/playback"
)

type cacheStatusService interface {
	CacheStatus(context.Context) (playback.CacheStatus, error)
}

func handleCacheStatus(ctx context.Context, w http.ResponseWriter, r *http.Request, service Service) bool {
	if r.URL.Path != "/v1/cache" {
		return false
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET for cache status")
		return true
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1))
	if r.URL.RawQuery != "" || err != nil || len(body) != 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "cache status takes no body or query parameters")
		return true
	}
	cache, ok := service.(cacheStatusService)
	if !ok {
		writeServiceError(w, &Error{Code: "unsupported", Message: "cache usage reporting is unavailable"})
		return true
	}
	result, err := cache.CacheStatus(ctx)
	writeResult(w, result, err)
	return true
}

func ReadCacheStatus(ctx context.Context, path string) (playback.CacheStatus, error) {
	var result playback.CacheStatus
	err := request(ctx, path, http.MethodGet, "/v1/cache", &result)
	return result, err
}
