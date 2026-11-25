package control

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/vadim82/panora/internal/playback"
)

const currentExclusionEndpoint = "/v1/exclusions/current"

type currentExclusionService interface {
	ExcludeCurrent(context.Context, string, playback.HistoryReference) (Status, error)
}

func handleCurrentExclusion(ctx context.Context, w http.ResponseWriter, r *http.Request, service Service) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST to exclude the observed current image")
		return
	}
	if body, err := io.ReadAll(io.LimitReader(r.Body, 1)); err != nil || len(body) != 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "current exclusion takes no body")
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	instance, history, image := query["instance"], query["history"], query["image"]
	if err != nil || len(query) != 3 || len(instance) != 1 || len(history) != 1 || len(image) != 1 {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "provide one instance, history ID and positive image ID")
		return
	}
	imageID, err := strconv.ParseInt(image[0], 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "image ID must be a positive integer")
		return
	}
	exclude, ok := service.(currentExclusionService)
	if !ok {
		writeAPIError(w, http.StatusNotImplemented, "unsupported", "current image exclusion is unavailable")
		return
	}
	status, err := exclude.ExcludeCurrent(ctx, instance[0], playback.HistoryReference{ID: history[0], ImageID: imageID})
	writeResult(w, status, err)
}

func ExcludeCurrent(ctx context.Context, path, instance string, current playback.HistoryReference) (Status, error) {
	query := url.Values{"instance": {instance}, "history": {current.ID}, "image": {strconv.FormatInt(current.ImageID, 10)}}
	var status Status
	err := request(ctx, path, http.MethodPost, currentExclusionEndpoint+"?"+query.Encode(), &status)
	return status, err
}
