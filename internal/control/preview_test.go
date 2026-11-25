package control

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vadim82/panora/internal/playback"
)

type previewFixture struct {
	testService
	requests int
	id       int64
	history  string
	err      error
}

func (s *previewFixture) Preview(_ context.Context, id int64) (playback.ImagePreview, error) {
	s.requests++
	s.id = id
	return playback.ImagePreview{Version: APIVersion, ImageID: id, Available: true, Width: 2, Height: 1, MIMEType: "image/png", Data: []byte("fixture")}, s.err
}

func (s *previewFixture) PreviewNeighbor(ctx context.Context, id int64, historyID string) (playback.ImagePreview, error) {
	s.history = historyID
	return s.Preview(ctx, id)
}

func TestPreviewRoutesRejectArbitraryPathsAndMutations(t *testing.T) {
	for _, tc := range []struct {
		method, path, body string
		status, requests   int
	}{
		{"GET", "/v1/images/1/preview", "", 200, 1},
		{"GET", "/v1/images/1/preview?history=history_id", "", 200, 1},
		{"GET", "/v1/images/1/preview?history=bad&history=also_bad", "", 400, 0},
		{"GET", "/v1/images/1/preview?history=%2Fetc%2Fpasswd", "", 400, 0},
		{"POST", "/v1/images/1/preview", "", 405, 0},
		{"GET", "/v1/images/1/preview", "{}", 400, 0},
		{"GET", "/v1/images/1/preview?path=/etc/passwd", "", 400, 0},
		{"GET", "/v1/images/1/preview?width=100000", "", 400, 0},
		{"GET", "/v1/images/../etc/passwd/preview", "", 400, 0},
		{"GET", "/v1/images/%2Fetc%2Fpasswd/preview", "", 400, 0},
		{"GET", "/v1/images/0/preview", "", 400, 0},
		{"GET", "/v1/images/-1/preview", "", 400, 0},
		{"GET", "/v1/images/+1/preview", "", 400, 0},
		{"GET", "/v1/images/01/preview", "", 400, 0},
		{"GET", "/v1/images/9223372036854775808/preview", "", 400, 0},
		{"GET", "/v1/images/1/preview/extra", "", 400, 0},
	} {
		t.Run(tc.method+tc.path+tc.body, func(t *testing.T) {
			fixture := &previewFixture{}
			response := httptest.NewRecorder()
			Handler(fixture).ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			var envelope struct {
				Version int
				Error   *Error
			}
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || response.Code != tc.status || envelope.Version != APIVersion || fixture.requests != tc.requests || fixture.actions != 0 {
				t.Fatalf("response=%d %s requests=%d %v", response.Code, response.Body.String(), fixture.requests, err)
			}
			if tc.status >= 400 && envelope.Error == nil {
				t.Fatal("missing typed error")
			}
			if tc.status == 200 && fixture.id != 1 {
				t.Fatal("image ID was not forwarded")
			}
			if tc.status == 200 && strings.Contains(tc.path, "?history=") && fixture.history != "history_id" {
				t.Fatal("history ID was not forwarded")
			}
		})
	}
	for _, tc := range []struct {
		code   string
		status int
	}{{"preview_not_found", 404}, {"busy", 409}, {"canceled", 408}, {"timeout", 504}} {
		fixture := &previewFixture{err: &Error{Code: tc.code, Message: "fixture"}}
		response := httptest.NewRecorder()
		Handler(fixture).ServeHTTP(response, httptest.NewRequest("GET", "/v1/images/1/preview", nil))
		if response.Code != tc.status {
			t.Fatalf("typed error=%d %s", response.Code, response.Body.String())
		}
	}
}
