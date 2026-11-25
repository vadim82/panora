package control

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/vadim82/panora/internal/playback"
)

type capabilityService interface {
	Capabilities(context.Context) (playback.Capabilities, error)
}

func handleCapabilities(ctx context.Context, w http.ResponseWriter, r *http.Request, service Service) bool {
	if r.URL.Path != "/v1/capabilities" {
		return false
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET for desktop capabilities")
		return true
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1))
	if r.URL.RawQuery != "" || err != nil || len(body) != 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "capabilities take no query parameters or body")
		return true
	}
	inspector, ok := service.(capabilityService)
	if !ok {
		writeServiceError(w, &Error{Code: "unsupported", Message: "desktop capability inspection is unavailable"})
		return true
	}
	result, err := inspector.Capabilities(ctx)
	if err != nil {
		var typed *Error
		switch {
		case errors.As(err, &typed):
			writeServiceError(w, typed)
		case errors.Is(err, context.Canceled):
			writeAPIError(w, http.StatusRequestTimeout, "canceled", "desktop capability inspection canceled")
		case errors.Is(err, context.DeadlineExceeded):
			writeAPIError(w, http.StatusGatewayTimeout, "timeout", "desktop capability inspection timed out")
		default:
			writeAPIError(w, http.StatusUnprocessableEntity, "capabilities_failed", "desktop capability inspection failed")
		}
	} else {
		_ = json.NewEncoder(w).Encode(result)
	}
	return true
}

func ReadCapabilities(ctx context.Context, path string) (playback.Capabilities, error) {
	var result playback.Capabilities
	err := request(ctx, path, http.MethodGet, "/v1/capabilities", &result)
	return result, err
}
