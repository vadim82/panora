package control

import (
	"context"
	"io"
	"net/http"

	"github.com/vadim82/panora/internal/playback"
)

type searchConfigurationService interface {
	AddSearch(context.Context, playback.AddSearchRequest) (playback.Configuration, error)
	UpdateSearch(context.Context, playback.UpdateSearchRequest) (playback.Configuration, error)
	RemoveSearch(context.Context, playback.RemoveSearchRequest) (playback.Configuration, error)
}

func handleSearchConfiguration(ctx context.Context, w http.ResponseWriter, r *http.Request, service Service) bool {
	if r.URL.Path != "/v1/configuration/searches" {
		return false
	}
	if r.Method != http.MethodPost && r.Method != http.MethodPatch && r.Method != http.MethodDelete {
		w.Header().Set("Allow", "POST, PATCH, DELETE")
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST, PATCH or DELETE for searches")
		return true
	}
	if r.URL.RawQuery != "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "search edits take no query parameters")
		return true
	}
	edits, ok := service.(searchConfigurationService)
	if !ok {
		writeServiceError(w, &Error{Code: "unsupported", Message: "this service does not support search editing"})
		return true
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 16385))
	if err != nil || len(body) > 16384 {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "invalid_request", "configuration request exceeds 16 KiB")
		return true
	}
	var result playback.Configuration
	switch r.Method {
	case http.MethodPost:
		var edit playback.AddSearchRequest
		if decodeConfigurationEdit(body, &edit) != nil || edit.Revision == "" {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "provide revision and supported search settings")
			return true
		}
		result, err = edits.AddSearch(ctx, edit)
	case http.MethodPatch:
		var edit playback.UpdateSearchRequest
		if decodeConfigurationEdit(body, &edit) != nil || edit.Revision == "" || edit.ID == "" {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "provide revision, id and supported search settings")
			return true
		}
		result, err = edits.UpdateSearch(ctx, edit)
	case http.MethodDelete:
		var edit playback.RemoveSearchRequest
		if decodeConfigurationEdit(body, &edit) != nil || edit.Revision == "" || edit.ID == "" {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "provide one revision/id object")
			return true
		}
		result, err = edits.RemoveSearch(ctx, edit)
	}
	writeResult(w, result, err)
	return true
}

func AddSearch(ctx context.Context, path string, edit playback.AddSearchRequest) (playback.Configuration, error) {
	return editConfiguration(ctx, path, http.MethodPost, "/v1/configuration/searches", edit)
}
func UpdateSearch(ctx context.Context, path string, edit playback.UpdateSearchRequest) (playback.Configuration, error) {
	return editConfiguration(ctx, path, http.MethodPatch, "/v1/configuration/searches", edit)
}
func RemoveSearch(ctx context.Context, path string, edit playback.RemoveSearchRequest) (playback.Configuration, error) {
	return editConfiguration(ctx, path, http.MethodDelete, "/v1/configuration/searches", edit)
}
