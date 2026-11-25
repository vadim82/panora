package control

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vadim82/panora/internal/playback"
)

type exclusionsFixture struct {
	testService
	before   int64
	limit    int
	restored int64
}

func (s *exclusionsFixture) ExcludeCurrent(_ context.Context, instance string, entry playback.HistoryReference) (Status, error) {
	if instance != "current" {
		return Status{}, &Error{Code: "instance_changed", Message: "instance changed"}
	}
	if entry != (playback.HistoryReference{ID: "accepted-1", ImageID: 7}) {
		return Status{}, &Error{Code: "current_changed", Message: "current image changed"}
	}
	s.actions++
	return Status{Version: APIVersion, CurrentExcluded: true}, nil
}

func TestCurrentExclusionRouteRejectsStaleAndMalformedRequests(t *testing.T) {
	service := &exclusionsFixture{}
	handler := Handler(service)
	for _, test := range []struct {
		query string
		code  int
	}{
		{"?instance=current&history=accepted-1&image=19", 409},
		{"?instance=old&history=accepted-1&image=7", 409},
		{"?instance=current&history=accepted-1&image=7&image=19", 400},
		{"?instance=current&history=accepted-1", 400},
		{"?instance=current&history=accepted-1&image=seven", 400},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("POST", currentExclusionEndpoint+test.query, nil))
		if response.Code != test.code || service.actions != 0 {
			t.Fatalf("query %q produced status %d and %d actions", test.query, response.Code, service.actions)
		}
	}
}

func (s *exclusionsFixture) Exclusions(_ context.Context, before int64, limit int) (playback.ExclusionPage, error) {
	s.before = before
	s.limit = limit
	return playback.ExclusionPage{Version: APIVersion, Entries: []playback.Exclusion{{ID: 12, Provider: "local", Image: "image.png", SourceID: "home"}}, NextBefore: 12}, nil
}
func (s *exclusionsFixture) RestoreExclusion(_ context.Context, id int64) (Status, error) {
	s.restored = id
	if id == 99 {
		return Status{}, &Error{Code: "exclusion_not_found", Message: "not found"}
	}
	return Status{Version: APIVersion}, nil
}

func TestExclusionRoutesValidateBoundsAndMutationBodies(t *testing.T) {
	for _, tt := range []struct {
		method, path, body string
		code               int
	}{
		{"GET", "/v1/exclusions", "", 200}, {"GET", "/v1/exclusions?before=15&limit=2", "", 200},
		{"GET", "/v1/exclusions?limit=11", "", 400}, {"GET", "/v1/exclusions?limit=0", "", 400}, {"GET", "/v1/exclusions?before=-1", "", 400}, {"GET", "/v1/exclusions?before=999999999999999999999999", "", 400},
		{"GET", "/v1/exclusions?limit=1&limit=2", "", 400}, {"GET", "/v1/exclusions?unknown=1", "", 400}, {"GET", "/v1/exclusions?before=%zz", "", 400},
		{"POST", "/v1/exclusions", "", 405}, {"POST", "/v1/actions/exclude", "", 200}, {"POST", "/v1/actions/undo-exclude", "", 200},
		{"POST", "/v1/actions/exclude", "{}", 400}, {"POST", "/v1/exclusions/12/restore", "", 200}, {"GET", "/v1/exclusions/12/restore", "", 405},
		{"POST", "/v1/exclusions/0/restore", "", 400}, {"POST", "/v1/exclusions/no/restore", "", 400}, {"POST", "/v1/exclusions/12/restore", "{}", 400}, {"POST", "/v1/exclusions/99/restore", "", 404},
	} {
		t.Run(tt.method+tt.path+tt.body, func(t *testing.T) {
			s := &exclusionsFixture{}
			response := httptest.NewRecorder()
			Handler(s).ServeHTTP(response, httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)))
			if response.Code != tt.code {
				t.Fatalf("status=%d want=%d body=%s", response.Code, tt.code, response.Body.String())
			}
			var envelope struct {
				Version int    `json:"version"`
				Error   *Error `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.Version != APIVersion {
				t.Fatalf("invalid envelope: %s %v", response.Body.String(), err)
			}
			if tt.code >= 400 && (envelope.Error == nil || s.actions != 0 || s.restored != 0 && s.restored != 99) {
				t.Fatal("invalid request was applied or missing error")
			}
			if tt.path == "/v1/exclusions?before=15&limit=2" && (s.before != 15 || s.limit != 2) {
				t.Fatal("pagination arguments lost")
			}
		})
	}
}
