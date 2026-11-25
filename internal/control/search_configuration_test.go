package control

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vadim82/panora/internal/playback"
)

type searchConfigurationFixture struct {
	configurationFixture
	added   playback.AddSearchRequest
	updated playback.UpdateSearchRequest
	removed playback.RemoveSearchRequest
}

func (s *searchConfigurationFixture) searchWrite(revision, id string) (playback.Configuration, error) {
	s.writes++
	if revision != "reviewed" {
		return playback.Configuration{}, &Error{Code: "config_conflict", Message: "reload settings"}
	}
	if id == "missing" {
		return playback.Configuration{}, &Error{Code: "search_not_found", Message: "reload settings"}
	}
	return s.Configuration(context.Background())
}
func (s *searchConfigurationFixture) AddSearch(_ context.Context, request playback.AddSearchRequest) (playback.Configuration, error) {
	s.added = request
	return s.searchWrite(request.Revision, "")
}
func (s *searchConfigurationFixture) UpdateSearch(_ context.Context, request playback.UpdateSearchRequest) (playback.Configuration, error) {
	s.updated = request
	return s.searchWrite(request.Revision, request.ID)
}
func (s *searchConfigurationFixture) RemoveSearch(_ context.Context, request playback.RemoveSearchRequest) (playback.Configuration, error) {
	s.removed = request
	return s.searchWrite(request.Revision, request.ID)
}

func TestSearchConfigurationRoutes(t *testing.T) {
	for _, tc := range []struct {
		method, body   string
		status, writes int
	}{
		{"POST", `{"revision":"reviewed","name":"Nature","query":"nature","categories":"110","sorting":"toplist","max_pages":1,"top_range":"1w","enabled":false}`, 200, 1},
		{"PATCH", `{"revision":"reviewed","id":"saved","name":"Edited","enabled":false}`, 200, 1},
		{"DELETE", `{"revision":"reviewed","id":"saved"}`, 200, 1},
		{"PATCH", `{"revision":"stale","id":"saved","query":"stars"}`, 409, 1},
		{"DELETE", `{"revision":"reviewed","id":"missing"}`, 404, 1},
		{"GET", ``, 405, 0},
		{"POST", `{"revision":"reviewed","name":"Nature","query":"nature","purity":"111"}`, 400, 0},
		{"POST", `{"revision":"reviewed","name":"Nature","query":"nature","apikey":"secret"}`, 400, 0},
		{"POST", `{"revision":"reviewed","query":"nature"}`, 200, 1},
		{"POST", `{"revision":"reviewed","name":"Nature"}`, 200, 1},
		{"PATCH", `{"revision":"reviewed","id":"saved","enabled":null}`, 400, 0},
		{"PATCH", `{"revision":"reviewed","id":"saved","enabled":false,"enabled":true}`, 400, 0},
		{"PATCH", `{"revision":"reviewed","id":"saved","ENABLED":false}`, 400, 0},
		{"PATCH", `{"revision":"reviewed","id":"saved","max_pages":1.5}`, 400, 0},
		{"PATCH", `{"revision":"reviewed","id":"saved","max_pages":"1"}`, 400, 0},
		{"PATCH", `{"revision":"reviewed","query":"stars"}`, 400, 0},
		{"DELETE", `{"revision":"reviewed","id":"saved","query":"stars"}`, 400, 0},
		{"DELETE", `{"id":"saved"}`, 400, 0},
		{"POST", `null`, 400, 0},
		{"POST", `{"revision":"reviewed","name":"Nature","query":"nature"} {}`, 400, 0},
		{"POST", strings.Repeat(" ", 16385), 413, 0},
	} {
		t.Run(tc.method+tc.body[:min(len(tc.body), 35)], func(t *testing.T) {
			fixture := &searchConfigurationFixture{}
			response := httptest.NewRecorder()
			Handler(fixture).ServeHTTP(response, httptest.NewRequest(tc.method, "/v1/configuration/searches", strings.NewReader(tc.body)))
			var envelope struct {
				Version int
				Error   *Error
			}
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.Version != 1 || response.Code != tc.status || fixture.writes != tc.writes {
				t.Fatalf("response=%d %s writes=%d %v", response.Code, response.Body.String(), fixture.writes, err)
			}
			if tc.status >= 400 && envelope.Error == nil {
				t.Fatal("missing typed error")
			}
			if tc.status == 200 {
				switch tc.method {
				case "POST":
					if fixture.added.Enabled != nil && (*fixture.added.Enabled || fixture.added.Categories != "110" || fixture.added.MaxPages == nil || *fixture.added.MaxPages != 1) {
						t.Fatalf("lost add fields=%+v", fixture.added)
					}
				case "PATCH":
					if fixture.updated.Enabled == nil || *fixture.updated.Enabled || fixture.updated.Name == nil || *fixture.updated.Name != "Edited" {
						t.Fatalf("lost patch fields=%+v", fixture.updated)
					}
				case "DELETE":
					if fixture.removed.ID != "saved" {
						t.Fatalf("lost removal ID=%+v", fixture.removed)
					}
				}
			}
		})
	}
	fixture := &searchConfigurationFixture{}
	response := httptest.NewRecorder()
	Handler(fixture).ServeHTTP(response, httptest.NewRequest("POST", "/v1/configuration/searches?unexpected=1", strings.NewReader(`{}`)))
	if response.Code != 400 || fixture.writes != 0 {
		t.Fatal("query parameters accepted")
	}
}
