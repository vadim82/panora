package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/vadim82/panora/internal/playback"
)

// Request never starts a service or retries a mutation after an uncertain result.
func Request(ctx context.Context, path, action string) (Status, error) {
	return RequestInstance(ctx, path, action, "")
}

// RequestInstance can bind a playback action to an observed service instance.
func RequestInstance(ctx context.Context, path, action, instance string) (Status, error) {
	method, endpoint := http.MethodGet, "/v1/status"
	if action != "" {
		if !validAction(action) {
			return Status{}, fmt.Errorf("unknown playback action %q", action)
		}
		method, endpoint = http.MethodPost, "/v1/actions/"+action
		if instance != "" {
			// A distinct route also rejects guarded actions on older services.
			endpoint += "/for-instance?instance=" + url.QueryEscape(instance)
		}
	}
	var status Status
	if err := request(ctx, path, method, endpoint, &status); err != nil {
		return Status{}, err
	}
	return status, nil
}

func ListExclusions(ctx context.Context, path string, before int64, limit int) (playback.ExclusionPage, error) {
	var page playback.ExclusionPage
	endpoint := "/v1/exclusions?before=" + strconv.FormatInt(before, 10) + "&limit=" + strconv.Itoa(limit)
	if err := request(ctx, path, http.MethodGet, endpoint, &page); err != nil {
		return page, err
	}
	return page, nil
}

func RestoreExclusion(ctx context.Context, path string, id int64) (Status, error) {
	if id <= 0 {
		return Status{}, &Error{Code: "invalid_request", Message: "exclusion ID must be positive"}
	}
	var status Status
	endpoint := "/v1/exclusions/" + strconv.FormatInt(id, 10) + "/restore"
	if err := request(ctx, path, http.MethodPost, endpoint, &status); err != nil {
		return Status{}, err
	}
	return status, nil
}

func request(ctx context.Context, path, method, endpoint string, target any) error {
	return requestBody(ctx, path, method, endpoint, nil, target, 1<<20)
}

func ReadConfiguration(ctx context.Context, path string) (playback.Configuration, error) {
	var config playback.Configuration
	err := requestBody(ctx, path, http.MethodGet, "/v1/configuration", nil, &config, playback.MaxConfigurationMessageBytes)
	return config, err
}

func AddFolder(ctx context.Context, path string, edit playback.AddFolderRequest) (playback.Configuration, error) {
	return editConfiguration(ctx, path, http.MethodPost, "/v1/configuration/folders", edit)
}

func UpdateConfiguration(ctx context.Context, path string, edit playback.UpdateConfigurationRequest) (playback.Configuration, error) {
	return editConfiguration(ctx, path, http.MethodPatch, "/v1/configuration", edit)
}

func UpdateFolder(ctx context.Context, path string, edit playback.UpdateFolderRequest) (playback.Configuration, error) {
	return editConfiguration(ctx, path, http.MethodPatch, "/v1/configuration/folders", edit)
}

func RemoveFolder(ctx context.Context, path string, edit playback.RemoveFolderRequest) (playback.Configuration, error) {
	return editConfiguration(ctx, path, http.MethodDelete, "/v1/configuration/folders", edit)
}

func editConfiguration(ctx context.Context, path, method, endpoint string, edit any) (playback.Configuration, error) {
	raw, err := json.Marshal(edit)
	if err != nil {
		return playback.Configuration{}, err
	}
	var config playback.Configuration
	err = requestBody(ctx, path, method, endpoint, raw, &config, playback.MaxConfigurationMessageBytes)
	return config, err
}

func requestBody(ctx context.Context, path, method, endpoint string, body []byte, target any, limit int64) error {
	return requestBodyWithDial(ctx, path, method, endpoint, body, target, limit, func(ctx context.Context, path string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	})
}

func requestBodyWithDial(ctx context.Context, path, method, endpoint string, body []byte, target any, limit int64, dial func(context.Context, string) (net.Conn, error)) error {
	if err := validateSocket(path); err != nil {
		return fmt.Errorf("Panora service unavailable: %w; start panora serve", err)
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dial(ctx, path)
		},
		DisableKeepAlives: true,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://panora"+endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	reconcile := "check panora status before retrying"
	if endpoint == "/v1/configuration" || endpoint == "/v1/configuration/folders" || endpoint == "/v1/configuration/searches" {
		reconcile = "reload settings before retrying"
	}
	if path, _, _ := strings.Cut(endpoint, "?"); path == "/v1/shutdown" {
		reconcile = "check whether the service stopped before retrying"
	}
	response, err := client.Do(req)
	if err != nil {
		var dialErr *net.OpError
		if method != http.MethodGet && endpoint != "/v1/rules/preview" && endpoint != commonsSearchEndpoint && !(errors.As(err, &dialErr) && dialErr.Op == "dial") {
			return fmt.Errorf("Panora request failed: %w; the action may have completed; %s", err, reconcile)
		}
		return fmt.Errorf("Panora service unavailable: %w; start panora serve", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return fmt.Errorf("read Panora response: %w", err)
	}
	if int64(len(raw)) > limit {
		return fmt.Errorf("Panora response exceeds %d bytes", limit)
	}
	var result struct {
		Version int    `json:"version"`
		Error   *Error `json:"error"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("read Panora response: %w; %s", err, reconcile)
	}
	if result.Version != APIVersion {
		return fmt.Errorf("incompatible Panora API version %d; expected %d", result.Version, APIVersion)
	}
	if result.Error != nil {
		return result.Error
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Panora service returned HTTP %d", response.StatusCode)
	}
	return json.Unmarshal(raw, target)
}
