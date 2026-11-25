package control

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/vadim82/panora/internal/playback"
)

func Handler(service Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		if r.URL.Path == currentExclusionEndpoint {
			handleCurrentExclusion(ctx, w, r, service)
			return
		}
		if handleNavigation(ctx, w, r, service) || handleOmarchyTheme(ctx, w, r, service) || handleRulePreview(ctx, w, r, service) || handleCacheStatus(ctx, w, r, service) || handlePreview(ctx, w, r, service) || handleCapabilities(ctx, w, r, service) || handleHistory(ctx, w, r, service) || handleConfiguration(ctx, w, r, service) {
			return
		}
		if r.URL.Path == "/v1/exclusions" {
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", "GET")
				writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET for exclusions")
				return
			}
			before, limit := int64(0), playback.MaxExclusionsPageSize
			query, parseErr := url.ParseQuery(r.URL.RawQuery)
			if parseErr != nil {
				writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid exclusions query")
				return
			}
			for key, values := range query {
				if len(values) != 1 || key != "before" && key != "limit" {
					writeAPIError(w, http.StatusBadRequest, "invalid_request", "use single before and limit values")
					return
				}
				value, err := strconv.ParseInt(values[0], 10, 64)
				if err != nil || value < 0 || key == "limit" && (value < 1 || value > playback.MaxExclusionsPageSize) {
					writeAPIError(w, http.StatusBadRequest, "invalid_request", "before must be nonnegative; limit must be 1 through 10")
					return
				}
				if key == "before" {
					before = value
				} else {
					limit = int(value)
				}
			}
			page, err := service.Exclusions(ctx, before, limit)
			writeResult(w, page, err)
			return
		}
		if r.URL.Path == "/v1/status" {
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", "GET")
				writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET for status")
				return
			}
			_ = json.NewEncoder(w).Encode(service.Status())
			return
		}
		action, actionPath := strings.CutPrefix(r.URL.Path, "/v1/actions/")
		action, checkInstance := strings.CutSuffix(action, "/for-instance")
		var restoreID int64
		if strings.HasPrefix(r.URL.Path, "/v1/exclusions/") && strings.HasSuffix(r.URL.Path, "/restore") {
			value := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/exclusions/"), "/restore")
			var err error
			restoreID, err = strconv.ParseInt(value, 10, 64)
			if err != nil || restoreID <= 0 {
				writeAPIError(w, http.StatusBadRequest, "invalid_request", "exclusion ID must be positive")
				return
			}
		} else if !actionPath || !validAction(action) {
			writeAPIError(w, http.StatusNotFound, "not_found", "unknown Panora API endpoint")
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST for playback actions")
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1))
		if err != nil || len(body) != 0 {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "playback actions take an empty body; configure panora serve instead")
			return
		}
		if actionPath && checkInstance {
			expected, valid := expectedInstance(r.URL.RawQuery)
			if !valid || expected == "" {
				writeAPIError(w, http.StatusBadRequest, "invalid_request", "a single service instance ID is required")
				return
			}
			if service.Status().InstanceID != expected {
				writeAPIError(w, http.StatusConflict, "instance_changed", "the connected Panora service changed before the action")
				return
			}
		}
		var status Status
		if restoreID > 0 {
			status, err = service.RestoreExclusion(ctx, restoreID)
		} else {
			status, err = service.Action(ctx, action)
		}
		writeResult(w, status, err)
	})
}
