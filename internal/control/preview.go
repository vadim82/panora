package control

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/vadim82/panora/internal/playback"
)

type previewService interface {
	Preview(context.Context, int64) (playback.ImagePreview, error)
}

type neighborPreviewService interface {
	PreviewNeighbor(context.Context, int64, string) (playback.ImagePreview, error)
}

func handlePreview(ctx context.Context, w http.ResponseWriter, r *http.Request, service Service) bool {
	if !strings.HasPrefix(r.URL.Path, "/v1/images/") {
		return false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/images/"), "/preview")
	imageID, err := strconv.ParseInt(id, 10, 64)
	if !strings.HasSuffix(r.URL.Path, "/preview") || err != nil || imageID < 1 || strconv.FormatInt(imageID, 10) != id {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "provide one positive image ID from history")
		return true
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET for image previews")
		return true
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1))
	query, queryErr := url.ParseQuery(r.URL.RawQuery)
	historyID := query.Get("history")
	if err != nil || len(body) != 0 || queryErr != nil ||
		r.URL.RawQuery != "" && (len(query) != 1 || len(query["history"]) != 1 || !playback.ValidHistoryReference(historyID)) {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "provide only an optional neighboring history ID")
		return true
	}
	var result playback.ImagePreview
	if historyID == "" {
		previews, ok := service.(previewService)
		if !ok {
			writeServiceError(w, &Error{Code: "unsupported", Message: "image previews are unavailable"})
			return true
		}
		result, err = previews.Preview(ctx, imageID)
	} else {
		previews, ok := service.(neighborPreviewService)
		if !ok {
			writeServiceError(w, &Error{Code: "unsupported", Message: "neighbor previews are unavailable"})
			return true
		}
		result, err = previews.PreviewNeighbor(ctx, imageID, historyID)
	}
	if err != nil {
		var typed *Error
		if errors.As(err, &typed) && typed.Code == "timeout" {
			writeAPIError(w, http.StatusGatewayTimeout, typed.Code, typed.Message)
		} else if errors.As(err, &typed) && typed.Code == "canceled" {
			writeAPIError(w, http.StatusRequestTimeout, typed.Code, typed.Message)
		} else {
			writeServiceError(w, err)
		}
	} else {
		_ = json.NewEncoder(w).Encode(result)
	}
	return true
}

func ReadPreview(ctx context.Context, path string, imageID int64) (playback.ImagePreview, error) {
	return readPreview(ctx, path, imageID, "")
}

func ReadNeighborPreview(ctx context.Context, path string, imageID int64, historyID string) (playback.ImagePreview, error) {
	if !playback.ValidHistoryReference(historyID) {
		return playback.ImagePreview{}, &Error{Code: "invalid_request", Message: "provide a valid history ID"}
	}
	return readPreview(ctx, path, imageID, "?history="+url.QueryEscape(historyID))
}

func readPreview(ctx context.Context, path string, imageID int64, query string) (playback.ImagePreview, error) {
	if imageID < 1 {
		return playback.ImagePreview{}, &Error{Code: "invalid_request", Message: "provide a positive image ID from history"}
	}
	var result playback.ImagePreview
	err := requestBody(ctx, path, http.MethodGet, "/v1/images/"+strconv.FormatInt(imageID, 10)+"/preview"+query, nil, &result, playback.MaxPreviewMessageBytes)
	return result, err
}
