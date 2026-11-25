package control

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vadim82/panora/internal/playback"
)

type capabilityFixture struct {
	testService
	err   error
	reads int
}

func (s *capabilityFixture) Capabilities(context.Context) (playback.Capabilities, error) {
	s.reads++
	return playback.Capabilities{Version: 1, State: "partial"}, s.err
}

func TestCapabilityRouteIsReadOnlyAndPreservesFailureCodes(t *testing.T) {
	for _, tc := range []struct {
		method, path, body string
		err                error
		status, reads      int
		code               string
	}{
		{"GET", "/v1/capabilities", "", nil, 200, 1, ""},
		{"POST", "/v1/capabilities", "", nil, 405, 0, "method_not_allowed"},
		{"GET", "/v1/capabilities?backend=omarchy", "", nil, 400, 0, "invalid_request"},
		{"GET", "/v1/capabilities", "{}", nil, 400, 0, "invalid_request"},
		{"GET", "/v1/capabilities", "", context.Canceled, 408, 1, "canceled"},
		{"GET", "/v1/capabilities", "", context.DeadlineExceeded, 504, 1, "timeout"},
		{"GET", "/v1/capabilities", "", errors.New("SECRET"), 422, 1, "capabilities_failed"},
	} {
		fixture := &capabilityFixture{err: tc.err}
		response := httptest.NewRecorder()
		Handler(fixture).ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
		var got struct {
			Version int
			Error   *Error
		}
		if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil || response.Code != tc.status || fixture.reads != tc.reads || fixture.actions != 0 || got.Version != 1 || strings.Contains(response.Body.String(), "SECRET") {
			t.Fatalf("response=%d %s reads=%d err=%v", response.Code, response.Body.String(), fixture.reads, err)
		}
		if tc.code != "" && (got.Error == nil || got.Error.Code != tc.code) {
			t.Fatalf("typed error=%+v", got.Error)
		}
	}
	response := httptest.NewRecorder()
	Handler(&testService{}).ServeHTTP(response, httptest.NewRequest("GET", "/v1/capabilities", nil))
	if response.Code != 501 {
		t.Fatalf("unsupported=%d %s", response.Code, response.Body.String())
	}
}
