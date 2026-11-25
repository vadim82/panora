package control

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
)

type ShutdownResult struct {
	Version    int    `json:"version"`
	Stopping   bool   `json:"stopping"`
	InstanceID string `json:"instance_id,omitempty"`
}

// HandlerWithShutdown adds a lifecycle operation owned by the service composer.
// Acknowledgement is flushed before stop is requested, including when a client
// disconnects during the response. The mutation is never replayed by the client.
func HandlerWithShutdown(service Service, stop func()) http.Handler {
	handler := Handler(service)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/shutdown" {
			handler.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST to request service shutdown")
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1))
		if err != nil || len(body) != 0 {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "shutdown takes no body")
			return
		}
		expected, valid := expectedInstance(r.URL.RawQuery)
		if !valid {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "shutdown accepts only an optional instance ID")
			return
		}
		if stop == nil {
			writeAPIError(w, http.StatusNotImplemented, "unsupported", "service shutdown is unavailable")
			return
		}
		// Once a complete valid request is accepted it remains accepted even if
		// the requester goes away before reading the response.
		result := ShutdownResult{Version: APIVersion, Stopping: true}
		if service != nil {
			result.InstanceID = service.Status().InstanceID
		}
		if expected != "" && result.InstanceID != expected {
			writeAPIError(w, http.StatusConflict, "instance_changed", "the connected Panora service changed before shutdown")
			return
		}
		defer stop()
		_ = json.NewEncoder(w).Encode(result)
		_ = http.NewResponseController(w).Flush()
	})
}

func expectedInstance(rawQuery string) (string, bool) {
	if rawQuery == "" {
		return "", true
	}
	query, err := url.ParseQuery(rawQuery)
	values := query["instance"]
	if err != nil || len(query) != 1 || len(values) != 1 || values[0] == "" || len(values[0]) > 128 {
		return "", false
	}
	return values[0], true
}

func Shutdown(ctx context.Context, path string) (ShutdownResult, error) {
	return ShutdownInstance(ctx, path, "")
}

// ShutdownInstance stops the service only if it still has the observed instance ID.
func ShutdownInstance(ctx context.Context, path, instance string) (ShutdownResult, error) {
	endpoint := "/v1/shutdown"
	if instance != "" {
		endpoint += "?instance=" + url.QueryEscape(instance)
	}
	var result ShutdownResult
	err := request(ctx, path, http.MethodPost, endpoint, &result)
	return result, err
}
