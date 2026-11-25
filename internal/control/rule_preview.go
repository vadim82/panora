package control

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/vadim82/panora/internal/playback"
)

type rulePreviewService interface {
	PreviewRules(context.Context, playback.RulePreviewRequest) (playback.RulePreview, error)
}

func handleRulePreview(ctx context.Context, w http.ResponseWriter, r *http.Request, service Service) bool {
	if r.URL.Path != "/v1/rules/preview" {
		return false
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST for draft rule previews")
		return true
	}
	if r.URL.RawQuery != "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "rule previews take no query parameters")
		return true
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 16385))
	if err != nil || len(body) > 16384 {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "invalid_request", "rule preview request exceeds 16 KiB")
		return true
	}
	var raw struct {
		Revision   string          `json:"revision"`
		ProviderID string          `json:"provider_id,omitempty"`
		InputID    string          `json:"input_id,omitempty"`
		Rules      json.RawMessage `json:"rules"`
	}
	var request playback.RulePreviewRequest
	if decodeConfigurationEdit(body, &raw) != nil || raw.Revision == "" || (raw.ProviderID == "") != (raw.InputID == "") || decodeConfigurationEdit(raw.Rules, &request.Rules) != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "provide revision and one rules object with supported fields; no duplicate or null fields")
		return true
	}
	request.Revision, request.ProviderID, request.InputID = raw.Revision, raw.ProviderID, raw.InputID
	previewer, ok := service.(rulePreviewService)
	if !ok {
		writeServiceError(w, &Error{Code: "unsupported", Message: "draft rule previews are unavailable"})
		return true
	}
	result, err := previewer.PreviewRules(ctx, request)
	writeResult(w, result, err)
	return true
}

func PreviewRules(ctx context.Context, path string, edit playback.RulePreviewRequest) (playback.RulePreview, error) {
	raw, err := json.Marshal(edit)
	if err != nil {
		return playback.RulePreview{}, err
	}
	var result playback.RulePreview
	err = requestBody(ctx, path, http.MethodPost, "/v1/rules/preview", raw, &result, 65536)
	return result, err
}
