package control

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/vadim82/panora/internal/playback"
)

const alphaCodersRoute = "/v1/providers/alpha-coders"

func handleAlphaCoders(ctx context.Context, w http.ResponseWriter, r *http.Request, service Service) bool {
	if r.URL.Path != alphaCodersRoute {
		return false
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST for Alpha Coders")
		return true
	}
	if r.URL.RawQuery != "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "this endpoint takes no query parameters")
		return true
	}
	editor, ok := service.(interface {
		AlphaCoders(context.Context, playback.AlphaRequest) (playback.AlphaResult, error)
	})
	if !ok {
		writeServiceError(w, &Error{Code: "unsupported", Message: "Alpha Coders is unavailable"})
		return true
	}
	const limit = 16 << 10
	body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil || len(body) > limit {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "invalid_request", "Alpha Coders request exceeds its size limit")
		return true
	}
	var req playback.AlphaRequest
	if decodeConfigurationEdit(body, &req) != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "provide a valid Alpha Coders request")
		return true
	}
	operation, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	result, err := editor.AlphaCoders(operation, req)
	writeResult(w, struct {
		Version int `json:"version"`
		playback.AlphaResult
	}{Version: APIVersion, AlphaResult: result}, err)
	return true
}

func AlphaCoders(ctx context.Context, path string, query playback.AlphaRequest) (playback.AlphaResult, error) {
	raw, err := json.Marshal(query)
	if err != nil {
		return playback.AlphaResult{}, err
	}
	var result playback.AlphaResult
	err = requestBody(ctx, path, http.MethodPost, alphaCodersRoute, raw, &result, 1<<20)
	return result, err
}
