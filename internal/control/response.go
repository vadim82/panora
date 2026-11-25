package control

import (
	"encoding/json"
	"errors"
	"net/http"
)

func writeResult(w http.ResponseWriter, result any, err error) {
	if err != nil {
		writeServiceError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Version int    `json:"version"`
		Error   *Error `json:"error"`
	}{APIVersion, &Error{Code: code, Message: message}})
}

func writeServiceError(w http.ResponseWriter, err error) {
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		apiErr = &Error{Code: "operation_failed", Message: err.Error()}
	}
	code := http.StatusUnprocessableEntity
	switch apiErr.Code {
	case "busy", "config_conflict", "current_changed", "instance_changed", "navigation_superseded":
		code = http.StatusConflict
	case "invalid_request":
		code = http.StatusBadRequest
	case "exclusion_not_found", "folder_not_found", "search_not_found", "history_not_found", "preview_not_found":
		code = http.StatusNotFound
	case "unsupported":
		code = http.StatusNotImplemented
	case "config_write_failed", "config_write_uncertain":
		code = http.StatusInternalServerError
	}
	writeAPIError(w, code, apiErr.Code, apiErr.Message)
}
