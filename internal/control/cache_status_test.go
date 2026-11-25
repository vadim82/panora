package control

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vadim82/panora/internal/cache"
	"github.com/vadim82/panora/internal/playback"
)

type cacheFixture struct {
	testService
	reads int
}

func (s *cacheFixture) CacheStatus(context.Context) (playback.CacheStatus, error) {
	s.reads++
	return playback.CacheStatus{Version: APIVersion, Saved: cache.Limits{MaxImages: 1, MaxBytes: 100}, Effective: cache.Limits{MaxImages: 1, MaxBytes: 100}, Usage: cache.Usage{State: "protected_over_budget", Images: 2, Bytes: 200, ProtectedImages: 2, ProtectedBytes: 200}}, nil
}

func TestCacheStatusRouteIsReadOnly(t *testing.T) {
	for _, tc := range []struct {
		method, path, body string
		status, reads      int
	}{
		{"GET", "/v1/cache", "", 200, 1}, {"POST", "/v1/cache", "", 405, 0}, {"GET", "/v1/cache?limit=1", "", 400, 0}, {"GET", "/v1/cache", "{}", 400, 0},
	} {
		fixture := &cacheFixture{}
		response := httptest.NewRecorder()
		Handler(fixture).ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
		var got struct {
			Version int
			Error   *Error
		}
		if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil || response.Code != tc.status || got.Version != APIVersion || fixture.reads != tc.reads || fixture.actions != 0 {
			t.Fatalf("response=%d %s reads=%d %v", response.Code, response.Body.String(), fixture.reads, err)
		}
		if tc.status >= 400 && got.Error == nil {
			t.Fatal("missing typed failure")
		}
	}
}
