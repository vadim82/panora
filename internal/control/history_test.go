package control

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vadim82/panora/internal/playback"
)

type historyFixture struct {
	testService
	before   string
	limit    int
	replayed string
}

func (s *historyFixture) History(_ context.Context, before string, limit int) (playback.HistoryPage, error) {
	s.before, s.limit = before, limit
	return playback.HistoryPage{Version: APIVersion, Entries: []playback.HistoryEntry{{ID: "saved-handle", ImageID: 42}}, NextBefore: "saved-handle"}, nil
}
func (s *historyFixture) ReplayHistory(_ context.Context, id string) (Status, error) {
	if id == "pruned" {
		return Status{}, &Error{Code: "history_not_found", Message: "entry pruned"}
	}
	s.replayed = id
	return Status{Version: APIVersion}, nil
}

func TestHistoryRoutesValidateQueriesBodiesAndMethods(t *testing.T) {
	for _, tc := range []struct {
		method, path, body string
		code               int
	}{
		{"GET", "/v1/history", "", 200}, {"GET", "/v1/history?before=older&limit=2", "", 200},
		{"GET", "/v1/history?limit=0", "", 400}, {"GET", "/v1/history?limit=11", "", 400},
		{"GET", "/v1/history?limit=2&limit=3", "", 400}, {"GET", "/v1/history?unknown=x", "", 400},
		{"GET", "/v1/history?before=%zz", "", 400}, {"POST", "/v1/history", "", 405},
		{"POST", "/v1/history/saved-handle/replay", "", 200}, {"GET", "/v1/history/saved-handle/replay", "", 405},
		{"POST", "/v1/history/saved-handle/replay", "{}", 400}, {"POST", "/v1/history/saved-handle/replay?x=1", "", 400},
		{"POST", "/v1/history//replay", "", 400}, {"POST", "/v1/history/a/b/replay", "", 400},
		{"POST", "/v1/history/pruned/replay", "", 404},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			fixture := &historyFixture{}
			response := httptest.NewRecorder()
			Handler(fixture).ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			var envelope struct {
				Version int
				Error   *Error
			}
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || response.Code != tc.code || envelope.Version != 1 {
				t.Fatalf("response=%d %s %v", response.Code, response.Body.String(), err)
			}
			if tc.code >= 400 && fixture.replayed != "" {
				t.Fatal("invalid replay reached service")
			}
			if tc.path == "/v1/history?before=older&limit=2" && (fixture.before != "older" || fixture.limit != 2) {
				t.Fatal("pagination lost")
			}
		})
	}
}
