package control

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/vadim82/panora/internal/playback"
)

const navigationEndpoint = "/v1/navigation"

type navigationService interface {
	Navigate(context.Context, playback.NavigationRequest) (Status, error)
	CancelNavigation(string, string) error
}

type cancelNavigationRequest struct {
	Instance string `json:"instance"`
	Token    string `json:"token"`
}

func handleNavigation(ctx context.Context, w http.ResponseWriter, r *http.Request, service Service) bool {
	if r.URL.Path != navigationEndpoint {
		return false
	}
	navigation, ok := service.(navigationService)
	if !ok {
		writeServiceError(w, &Error{Code: "unsupported", Message: "this service does not support navigation requests"})
		return true
	}
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		w.Header().Set("Allow", "POST, DELETE")
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST or DELETE for navigation")
		return true
	}
	const maxBody = 512
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err == nil && len(body) <= maxBody && r.URL.RawQuery == "" {
		if r.Method == http.MethodPost {
			var input playback.NavigationRequest
			if decodeConfigurationEdit(body, &input) == nil {
				status, err := navigation.Navigate(ctx, input)
				writeResult(w, status, err)
				return true
			}
		} else {
			var input cancelNavigationRequest
			if decodeConfigurationEdit(body, &input) == nil {
				err := navigation.CancelNavigation(input.Instance, input.Token)
				writeResult(w, struct {
					Version int `json:"version"`
				}{APIVersion}, err)
				return true
			}
		}
	}
	writeAPIError(w, http.StatusBadRequest, "invalid_request", "provide a valid navigation request")
	return true
}

func Navigate(ctx context.Context, path string, input playback.NavigationRequest) (Status, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return Status{}, err
	}
	var status Status
	err = requestBody(ctx, path, http.MethodPost, navigationEndpoint, raw, &status, 1<<20)
	return status, err
}

func CancelNavigation(ctx context.Context, path, instance, token string) error {
	raw, err := json.Marshal(cancelNavigationRequest{Instance: instance, Token: token})
	if err != nil {
		return err
	}
	return requestBody(ctx, path, http.MethodDelete, navigationEndpoint, raw, &struct{}{}, 512)
}
