package control

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vadim82/panora/internal/playback"
)

type testService struct{ actions int }

func (*testService) Status() Status {
	return Status{Version: APIVersion, InstanceID: "current", Paused: true}
}
func (s *testService) Action(_ context.Context, action string) (Status, error) {
	s.actions++
	if action == "previous" {
		return Status{}, &Error{Code: "no_previous", Message: "no previous image"}
	}
	if action == "resume" {
		return Status{}, &Error{Code: "busy", Message: "busy"}
	}
	return s.Status(), nil
}

func TestHandlerVersionedRoutesAndErrors(t *testing.T) {
	for _, tt := range []struct {
		method, path, body string
		code               int
		actions            int
		errorCode          string
	}{
		{"GET", "/v1/status", "", 200, 0, ""},
		{"POST", "/v1/actions/next", "", 200, 1, ""},
		{"POST", "/v1/actions/next/for-instance?instance=current", "", 200, 1, ""},
		{"POST", "/v1/actions/next/for-instance?instance=old", "", 409, 0, "instance_changed"},
		{"POST", "/v1/actions/next/for-instance", "", 400, 0, "invalid_request"},
		{"POST", "/v1/actions/next/for-instance?instance=current&instance=old", "", 400, 0, "invalid_request"},
		{"POST", "/v1/actions/pause", "", 200, 1, ""},
		{"POST", "/v1/actions/previous", "", 422, 1, "no_previous"},
		{"POST", "/v1/actions/resume", "", 409, 1, "busy"},
		{"GET", "/v1/actions/next", "", 405, 0, "method_not_allowed"},
		{"POST", "/v1/status", "", 405, 0, "method_not_allowed"},
		{"POST", "/v2/actions/next", "", 404, 0, "not_found"},
		{"POST", "/v1/actions/unknown", "", 404, 0, "not_found"},
		{"POST", "/v1/actions/next", strings.Repeat("x", 10000), 400, 0, "invalid_request"},
	} {
		t.Run(tt.method+tt.path+tt.errorCode, func(t *testing.T) {
			service := &testService{}
			response := httptest.NewRecorder()
			Handler(service).ServeHTTP(response, httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)))
			var result struct {
				Version int    `json:"version"`
				Error   *Error `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if response.Code != tt.code || result.Version != APIVersion || service.actions != tt.actions {
				t.Fatalf("code=%d version=%d actions=%d body=%s", response.Code, result.Version, service.actions, response.Body.String())
			}
			if tt.errorCode != "" && (result.Error == nil || result.Error.Code != tt.errorCode) {
				t.Fatalf("unexpected API error: %+v", result.Error)
			}
			if response.Header().Get("Content-Type") != "application/json" {
				t.Fatal("missing JSON content type")
			}
		})
	}
}

type healthFixtureService struct{ testService }

func (*healthFixtureService) Status() Status {
	return Status{Version: APIVersion, RefreshHealthVersion: 1, Providers: []playback.ProviderRefreshHealth{{ID: "wallhaven", Enabled: true, State: "empty", TotalInputs: 1, Inputs: []playback.InputRefreshHealth{{ID: "nature", Revision: "rev", State: "empty", Interval: "5m0s", Counts: &playback.RefreshCounts{Scope: "committed_metadata_in_attempt", Pages: 1}}}}}}
}

func TestRefreshHealthIsAdditiveToVersionOneStatus(t *testing.T) {
	response := httptest.NewRecorder()
	Handler(&healthFixtureService{}).ServeHTTP(response, httptest.NewRequest("GET", "/v1/status", nil))
	var oldClient struct {
		Version int  `json:"version"`
		Paused  bool `json:"paused"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &oldClient); err != nil || oldClient.Version != 1 {
		t.Fatalf("v1 compatibility: %v %s", err, response.Body.String())
	}
	var result Status
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.RefreshHealthVersion != 1 || result.Providers[0].Inputs[0].Counts.KnownEligible != nil || !strings.Contains(response.Body.String(), `"known_eligible":null`) {
		t.Fatalf("unknown metadata eligibility: %s", response.Body.String())
	}
}

func (s *testService) Exclusions(context.Context, int64, int) (playback.ExclusionPage, error) {
	return playback.ExclusionPage{Version: APIVersion, Entries: []playback.Exclusion{}}, nil
}
func (s *testService) RestoreExclusion(context.Context, int64) (Status, error) {
	s.actions++
	return s.Status(), nil
}
