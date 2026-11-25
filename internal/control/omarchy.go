package control

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/vadim82/panora/internal/playback"
)

type omarchyThemeService interface {
	EditOmarchyTheme(context.Context, string, playback.EditOmarchyThemeRequest) (playback.Configuration, error)
	OmarchyThemePreview(context.Context, int, int) (playback.OmarchyThemePreview, error)
}

func handleOmarchyTheme(ctx context.Context, w http.ResponseWriter, r *http.Request, adapter Service) bool {
	if r.URL.Path != "/v1/omarchy-theme" && r.URL.Path != "/v1/configuration/omarchy-theme" {
		return false
	}
	service, ok := adapter.(omarchyThemeService)
	if !ok {
		writeServiceError(w, &Error{Code: "unsupported", Message: "this service does not support Omarchy theme sources"})
		return true
	}
	if r.URL.Path == "/v1/omarchy-theme" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET for the Omarchy theme preview")
			return true
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1))
		query, parseErr := url.ParseQuery(r.URL.RawQuery)
		page, limit := 1, 4
		valid := err == nil && len(body) == 0 && parseErr == nil
		for key, values := range query {
			if len(values) != 1 || key != "page" && key != "limit" {
				valid = false
				continue
			}
			value, err := strconv.Atoi(values[0])
			if err != nil {
				valid = false
				continue
			}
			if key == "page" {
				page = value
			} else {
				limit = value
			}
		}
		if !valid {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "use single page and limit values and no request body")
			return true
		}
		result, err := service.OmarchyThemePreview(ctx, page, limit)
		writeResult(w, result, err)
		return true
	}

	action := omarchyEditAction(r.Method)
	if action == "" {
		w.Header().Set("Allow", "POST, PATCH, DELETE")
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST, PATCH or DELETE for the Omarchy theme source")
		return true
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 16385))
	if err != nil || len(body) > 16384 {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "invalid_request", "configuration request exceeds 16 KiB")
		return true
	}
	var edit playback.EditOmarchyThemeRequest
	if r.URL.RawQuery != "" || decodeConfigurationEdit(body, &edit) != nil || edit.Revision == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "provide supported source settings with the configuration revision and no query")
		return true
	}
	result, err := service.EditOmarchyTheme(ctx, action, edit)
	writeResult(w, result, err)
	return true
}

func omarchyEditAction(method string) string {
	switch method {
	case http.MethodPost:
		return "add"
	case http.MethodPatch:
		return "update"
	case http.MethodDelete:
		return "remove"
	}
	return ""
}

func EditOmarchyTheme(ctx context.Context, socket, method string, edit playback.EditOmarchyThemeRequest) (playback.Configuration, error) {
	if omarchyEditAction(method) == "" {
		return playback.Configuration{}, fmt.Errorf("invalid Omarchy theme source method")
	}
	return editConfiguration(ctx, socket, method, "/v1/configuration/omarchy-theme", edit)
}

func ReadOmarchyTheme(ctx context.Context, socket string, page, limit int) (playback.OmarchyThemePreview, error) {
	var result playback.OmarchyThemePreview
	err := requestBody(ctx, socket, http.MethodGet, fmt.Sprintf("/v1/omarchy-theme?page=%d&limit=%d", page, limit),
		nil, &result, playback.MaxOmarchyThemeMessageBytes)
	return result, err
}
