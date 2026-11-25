package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/vadim82/panora/internal/playback"
)

type configurationService interface {
	Configuration(context.Context) (playback.Configuration, error)
	AddFolder(context.Context, playback.AddFolderRequest) (playback.Configuration, error)
	UpdateConfiguration(context.Context, playback.UpdateConfigurationRequest) (playback.Configuration, error)
	UpdateFolder(context.Context, playback.UpdateFolderRequest) (playback.Configuration, error)
	RemoveFolder(context.Context, playback.RemoveFolderRequest) (playback.Configuration, error)
}

func handleConfiguration(ctx context.Context, w http.ResponseWriter, r *http.Request, service Service) bool {
	if handleSearchConfiguration(ctx, w, r, service) || handleProviderEditor(ctx, w, r, service) {
		return true
	}
	if r.URL.Path != "/v1/configuration" && r.URL.Path != "/v1/configuration/folders" {
		return false
	}
	configuration, ok := service.(configurationService)
	if !ok {
		writeServiceError(w, &Error{Code: "unsupported", Message: "this service does not support configuration editing"})
		return true
	}
	allowed := "GET, PATCH"
	valid := r.Method == http.MethodGet || r.Method == http.MethodPatch
	if r.URL.Path == "/v1/configuration/folders" {
		allowed = "POST, PATCH, DELETE"
		valid = r.Method == http.MethodPost || r.Method == http.MethodPatch || r.Method == http.MethodDelete
	}
	if !valid {
		w.Header().Set("Allow", allowed)
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use "+allowed+" for this configuration endpoint")
		return true
	}
	if r.URL.RawQuery != "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "configuration endpoints take no query parameters")
		return true
	}
	var result playback.Configuration
	var err error
	if r.Method == http.MethodGet {
		result, err = configuration.Configuration(ctx)
	} else {
		body, readErr := io.ReadAll(io.LimitReader(r.Body, 16385))
		if readErr != nil || len(body) > 16384 {
			writeAPIError(w, http.StatusRequestEntityTooLarge, "invalid_request", "configuration request exceeds 16 KiB")
			return true
		}
		if r.URL.Path == "/v1/configuration" {
			var edit playback.UpdateConfigurationRequest
			if err := decodeConfigurationEdit(body, &edit); err != nil || edit.Revision == "" {
				writeAPIError(w, http.StatusBadRequest, "invalid_request", "provide one JSON object with revision and supported settings; no duplicate or null fields")
				return true
			}
			result, err = configuration.UpdateConfiguration(ctx, edit)
		} else if r.Method == http.MethodPatch {
			var edit playback.UpdateFolderRequest
			if err := decodeConfigurationEdit(body, &edit); err != nil || edit.Revision == "" || edit.ID == "" {
				writeAPIError(w, http.StatusBadRequest, "invalid_request", "provide revision, id and supported folder settings")
				return true
			}
			result, err = configuration.UpdateFolder(ctx, edit)
		} else if r.Method == http.MethodDelete {
			var edit playback.RemoveFolderRequest
			if err := decodeConfigurationEdit(body, &edit); err != nil || edit.Revision == "" || edit.ID == "" {
				writeAPIError(w, http.StatusBadRequest, "invalid_request", "provide one revision/id object")
				return true
			}
			result, err = configuration.RemoveFolder(ctx, edit)
		} else {
			var edit playback.AddFolderRequest
			if err := decodeConfigurationEdit(body, &edit); err != nil || edit.Revision == "" || edit.Path == "" {
				writeAPIError(w, http.StatusBadRequest, "invalid_request", "provide one revision/path object; no duplicate or null fields")
				return true
			}
			result, err = configuration.AddFolder(ctx, edit)
		}
	}
	writeResult(w, result, err)
	return true
}

// encoding/json rejects unknown fields and wrong types, but accepts duplicate
// keys and null scalars. Reject those before decoding a bounded edit document.
func decodeConfigurationEdit(body []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return errors.New("expected an object")
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok || key != strings.ToLower(key) || seen[key] {
			return errors.New("duplicate or case-mismatched setting")
		}
		seen[key] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errors.New("null setting")
		}
		if key == "image_rules" {
			var rules playback.SourceImageRules
			if err := decodeConfigurationEdit(value, &rules); err != nil {
				return err
			}
		}
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("expected exactly one object")
	}
	decoder = json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
