package control

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/vadim82/panora/internal/playback"
)

type historyService interface {
	History(context.Context, string, int) (playback.HistoryPage, error)
	ReplayHistory(context.Context, string) (Status, error)
}

func handleHistory(ctx context.Context, w http.ResponseWriter, r *http.Request, service Service) bool {
	if r.URL.Path != "/v1/history" && !strings.HasPrefix(r.URL.Path, "/v1/history/") {
		return false
	}
	history, ok := service.(historyService)
	if !ok {
		writeServiceError(w, &Error{Code: "unsupported", Message: "this service does not support history"})
		return true
	}
	if r.URL.Path == "/v1/history" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET for history")
			return true
		}
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid history query")
			return true
		}
		before, limit := "", playback.MaxHistoryPageSize
		for key, values := range query {
			if len(values) != 1 || key != "before" && key != "limit" {
				writeAPIError(w, http.StatusBadRequest, "invalid_request", "use single before and limit values")
				return true
			}
			if key == "before" {
				before = values[0]
			} else {
				limit, err = strconv.Atoi(values[0])
				if err != nil || limit < 1 || limit > playback.MaxHistoryPageSize {
					writeAPIError(w, http.StatusBadRequest, "invalid_request", "history limit must be 1 through 10")
					return true
				}
			}
		}
		page, err := history.History(ctx, before, limit)
		writeResult(w, page, err)
		return true
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/history/"), "/replay")
	if !strings.HasSuffix(r.URL.Path, "/replay") || id == "" || strings.Contains(id, "/") || len(id) > 64 {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "provide one history ID to replay")
		return true
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST for replay")
		return true
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1))
	if err != nil || len(body) != 0 || r.URL.RawQuery != "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "replay takes an empty body and no query parameters")
		return true
	}
	status, err := history.ReplayHistory(ctx, id)
	writeResult(w, status, err)
	return true
}

func ListHistory(ctx context.Context, path, before string, limit int) (playback.HistoryPage, error) {
	var page playback.HistoryPage
	query := url.Values{"before": {before}, "limit": {strconv.Itoa(limit)}}
	err := requestBody(ctx, path, http.MethodGet, "/v1/history?"+query.Encode(), nil, &page, playback.MaxHistoryMessageBytes)
	return page, err
}

func ReplayHistory(ctx context.Context, path, id string) (Status, error) {
	if id == "" || len(id) > 64 || strings.Contains(id, "/") {
		return Status{}, &Error{Code: "invalid_request", Message: "provide a history ID from panora history ls"}
	}
	var status Status
	err := request(ctx, path, http.MethodPost, "/v1/history/"+url.PathEscape(id)+"/replay", &status)
	return status, err
}
