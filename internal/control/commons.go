package control

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/vadim82/panora/internal/source"
)

const commonsSearchEndpoint = "/v1/providers/commons/search"

type commonsSearchService interface {
	SearchCommons(context.Context, source.CommonsSearch) (source.CommonsSearchPage, error)
}

func handleCommonsSearch(ctx context.Context, w http.ResponseWriter, r *http.Request, s Service) bool {
	if r.URL.Path != commonsSearchEndpoint {
		return false
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST for a Commons search")
		return true
	}
	provider, ok := s.(commonsSearchService)
	if !ok {
		writeServiceError(w, &Error{Code: "unsupported", Message: "this service does not support Commons search"})
		return true
	}
	const limit = 2048
	raw, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	var query source.CommonsSearch
	if err != nil || len(raw) > limit || r.URL.RawQuery != "" || decodeConfigurationEdit(raw, &query) != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "provide bounded Commons search settings")
		return true
	}
	if err := query.Validate(); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return true
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	page, err := provider.SearchCommons(ctx, query)
	writeResult(w, page, err)
	return true
}

func SearchCommons(ctx context.Context, path string, query source.CommonsSearch) (source.CommonsSearchPage, error) {
	raw, err := json.Marshal(query)
	if err != nil {
		return source.CommonsSearchPage{}, err
	}
	var page source.CommonsSearchPage
	err = requestBody(ctx, path, http.MethodPost, commonsSearchEndpoint, raw, &page, source.MaxResponseBytes)
	return page, err
}
