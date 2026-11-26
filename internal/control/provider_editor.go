package control

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/vadim82/panora/internal/playback"
	"github.com/vadim82/panora/internal/source"
)

const (
	providerInputRoute   = "/v1/configuration/inputs"
	providerSuggestRoute = "/v1/providers/suggest"
)

type providerInputService interface {
	AddProviderInput(context.Context, playback.ProviderInputEdit) (playback.Configuration, error)
	UpdateProviderInput(context.Context, playback.ProviderInputEdit) (playback.Configuration, error)
	RemoveProviderInput(context.Context, playback.ProviderInputEdit) (playback.Configuration, error)
	SuggestProviderInput(context.Context, string, source.SuggestParams) ([]string, error)
}

type ProviderSuggestRequest struct {
	ProviderID string `json:"provider_id"`
	source.SuggestParams
}

type ProviderSuggestResult struct {
	Version int      `json:"version"`
	Values  []string `json:"values"`
}

func handleProviderEditor(ctx context.Context, w http.ResponseWriter, r *http.Request, service Service) bool {
	suggest := r.URL.Path == providerSuggestRoute
	if !suggest && r.URL.Path != providerInputRoute {
		return false
	}
	if r.Method != http.MethodPost && (suggest || r.Method != http.MethodPatch && r.Method != http.MethodDelete) {
		allowed := "POST, PATCH, DELETE"
		if suggest {
			allowed = "POST"
		}
		w.Header().Set("Allow", allowed)
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use "+allowed+" for this endpoint")
		return true
	}
	if r.URL.RawQuery != "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "this endpoint takes no query parameters")
		return true
	}
	editor, ok := service.(providerInputService)
	if !ok {
		writeServiceError(w, &Error{Code: "unsupported", Message: "this service does not support installed provider editors"})
		return true
	}
	limit := int64(16384)
	if suggest {
		limit = 1024
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil || int64(len(body)) > limit {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "invalid_request", "provider editor request exceeds its size limit")
		return true
	}
	if suggest {
		var query ProviderSuggestRequest
		if decodeConfigurationEdit(body, &query) != nil || !source.SupportsProviderInputs(query.ProviderID) || !query.SuggestParams.Valid() {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "provide a provider ID, field, prefix and limit")
			return true
		}
		queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		values, err := editor.SuggestProviderInput(queryCtx, query.ProviderID, query.SuggestParams)
		if err != nil {
			writeServiceError(w, &Error{Code: "unavailable", Message: "provider suggestions are unavailable"})
		} else {
			_ = json.NewEncoder(w).Encode(ProviderSuggestResult{Version: APIVersion, Values: values})
		}
		return true
	}
	var edit playback.ProviderInputEdit
	if decodeConfigurationEdit(body, &edit) != nil || edit.Revision == "" || edit.ProviderID == "" ||
		(r.Method == http.MethodPost) != (edit.ID == "") ||
		r.Method == http.MethodDelete && (edit.Name != nil || edit.Enabled != nil || edit.Values != nil || edit.ImageRules != nil || edit.InheritRules != nil) {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "provide a revision, provider ID and the settings supported by this method")
		return true
	}
	var result playback.Configuration
	switch r.Method {
	case http.MethodPost:
		result, err = editor.AddProviderInput(ctx, edit)
	case http.MethodPatch:
		result, err = editor.UpdateProviderInput(ctx, edit)
	case http.MethodDelete:
		result, err = editor.RemoveProviderInput(ctx, edit)
	}
	writeResult(w, result, err)
	return true
}

func EditProviderInput(ctx context.Context, path, method string, edit playback.ProviderInputEdit) (playback.Configuration, error) {
	return editConfiguration(ctx, path, method, providerInputRoute, edit)
}

func SuggestProviderInput(ctx context.Context, path string, query ProviderSuggestRequest) (ProviderSuggestResult, error) {
	raw, err := json.Marshal(query)
	if err != nil {
		return ProviderSuggestResult{}, err
	}
	var result ProviderSuggestResult
	err = requestBody(ctx, path, http.MethodPost, providerSuggestRoute, raw, &result, 8192)
	return result, err
}
