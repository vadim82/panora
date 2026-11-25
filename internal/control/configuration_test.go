package control

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vadim82/panora/internal/playback"
)

type configurationFixture struct {
	testService
	writes int
	edit   playback.AddFolderRequest
	result *playback.Configuration
}

func (s *configurationFixture) Configuration(context.Context) (playback.Configuration, error) {
	if s.result != nil {
		return *s.result, nil
	}
	return playback.Configuration{Version: APIVersion, Revision: "reviewed", TOML: "version=1\n"}, nil
}
func (s *configurationFixture) AddFolder(_ context.Context, edit playback.AddFolderRequest) (playback.Configuration, error) {
	s.writes++
	s.edit = edit
	if edit.Revision != "reviewed" {
		return playback.Configuration{}, &Error{Code: "config_conflict", Message: "reload settings"}
	}
	return s.Configuration(context.Background())
}

func (s *configurationFixture) UpdateConfiguration(_ context.Context, edit playback.UpdateConfigurationRequest) (playback.Configuration, error) {
	s.writes++
	if edit.Revision != "reviewed" {
		return playback.Configuration{}, &Error{Code: "config_conflict", Message: "reload settings"}
	}
	return s.Configuration(context.Background())
}

func (s *configurationFixture) UpdateFolder(_ context.Context, edit playback.UpdateFolderRequest) (playback.Configuration, error) {
	s.writes++
	if edit.Revision != "reviewed" {
		return playback.Configuration{}, &Error{Code: "config_conflict", Message: "reload settings"}
	}
	return s.Configuration(context.Background())
}

func (s *configurationFixture) RemoveFolder(_ context.Context, edit playback.RemoveFolderRequest) (playback.Configuration, error) {
	s.writes++
	if edit.ID == "missing" {
		return playback.Configuration{}, &Error{Code: "folder_not_found", Message: "reload settings"}
	}
	return s.Configuration(context.Background())
}

func TestConfigurationRoutesValidateBeforeSaving(t *testing.T) {
	for _, tc := range []struct {
		method, path, body string
		status, writes     int
	}{
		{"GET", "/v1/configuration", "", 200, 0},
		{"PATCH", "/v1/configuration/folders", `{"revision":"reviewed","id":"home","enabled":false,"max_depth":0}`, 200, 1},
		{"PATCH", "/v1/configuration/folders", `{"revision":"stale","id":"home","name":"New"}`, 409, 1},
		{"PATCH", "/v1/configuration/folders", `{"revision":"reviewed","name":"New"}`, 400, 0},
		{"PATCH", "/v1/configuration/folders", `{"revision":"reviewed","id":"home","enabled":null}`, 400, 0},
		{"PATCH", "/v1/configuration/folders", `{"revision":"reviewed","id":"home","enabled":false,"enabled":true}`, 400, 0},
		{"PATCH", "/v1/configuration/folders", `{"revision":"reviewed","id":"home","max_depth":"0"}`, 400, 0},
		{"DELETE", "/v1/configuration/folders", `{"revision":"reviewed","id":"home"}`, 200, 1},
		{"DELETE", "/v1/configuration/folders", `{"revision":"reviewed","id":"missing"}`, 404, 1},
		{"DELETE", "/v1/configuration/folders", `{"revision":"reviewed","id":"home","path":"/x"}`, 400, 0},
		{"DELETE", "/v1/configuration/folders", `{"revision":"reviewed"}`, 400, 0},
		{"PATCH", "/v1/configuration", `{"revision":"reviewed","min_width":1920}`, 200, 1},
		{"PATCH", "/v1/configuration", `{"revision":"reviewed","ratio":"presets","ratio_presets":["16:9","21:9"]}`, 200, 1},
		{"PATCH", "/v1/configuration", `{"revision":"reviewed","ratio_presets":null}`, 400, 0},
		{"PATCH", "/v1/configuration", `{"revision":"reviewed","ratio_presets":"16:9"}`, 400, 0},
		{"PATCH", "/v1/configuration", `{"revision":"stale","min_width":1920}`, 409, 1},
		{"PATCH", "/v1/configuration", `{"revision":"reviewed","providers":[]}`, 400, 0},
		{"PATCH", "/v1/configuration", `{"revision":"reviewed","MIN_WIDTH":1920}`, 400, 0},
		{"PATCH", "/v1/configuration", `{"revision":"reviewed","version":2}`, 400, 0},
		{"PATCH", "/v1/configuration", `{"revision":"reviewed","toml":"version=1"}`, 400, 0},
		{"PATCH", "/v1/configuration", `{"revision":"reviewed","min_width":2,"min_width":3}`, 400, 0},
		{"PATCH", "/v1/configuration", `{"revision":"reviewed","min_width":null}`, 400, 0},
		{"PATCH", "/v1/configuration", `{"revision":"reviewed","upscale":"false"}`, 400, 0},
		{"PATCH", "/v1/configuration", `{"revision":"reviewed","min_width":1.5}`, 400, 0},
		{"PATCH", "/v1/configuration", `{"revision":"reviewed","min_width":9223372036854775808}`, 400, 0},
		{"PATCH", "/v1/configuration", `{"min_width":1920}`, 400, 0},
		{"PATCH", "/v1/configuration", `null`, 400, 0},
		{"PATCH", "/v1/configuration", `{"revision":"reviewed","min_width":1920} {}`, 400, 0},
		{"PATCH", "/v1/configuration", strings.Repeat(" ", 16385), 413, 0},
		{"POST", "/v1/configuration/folders", `{"revision":"reviewed","path":"/wallpapers"}`, 200, 1},
		{"POST", "/v1/configuration/folders", `{"revision":"old","path":"/wallpapers"}`, 409, 1},
		{"POST", "/v1/configuration", "", 405, 0}, {"GET", "/v1/configuration/folders", "", 405, 0},
		{"GET", "/v1/configuration?extra=1", "", 400, 0},
		{"POST", "/v1/configuration/folders", `{"revision":"r","path":"/x","unknown":1}`, 400, 0},
		{"POST", "/v1/configuration/folders", `{"revision":"r","path":"/x"} {}`, 400, 0},
		{"POST", "/v1/configuration/folders", `{"path":"/x"}`, 400, 0},
		{"POST", "/v1/configuration/folders", `null`, 400, 0},
		{"POST", "/v1/configuration/folders", strings.Repeat(" ", 16385), 413, 0},
	} {
		t.Run(tc.method+tc.path+tc.body[:min(30, len(tc.body))], func(t *testing.T) {
			fixture := &configurationFixture{}
			response := httptest.NewRecorder()
			Handler(fixture).ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			var envelope struct {
				Version int
				Error   *Error
			}
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.Version != 1 || response.Code != tc.status || fixture.writes != tc.writes {
				t.Fatalf("response=%d %s writes=%d error=%v", response.Code, response.Body.String(), fixture.writes, err)
			}
			if tc.status >= 400 && envelope.Error == nil {
				t.Fatal("missing typed error")
			}
		})
	}
}

func TestSourceRulesTransportRejectsNestedAmbiguityBeforeSaving(t *testing.T) {
	for _, rule := range []string{
		`{"min_width":1,"min_width":2,"min_height":1,"ratio":"any"}`,
		`{"min_width":1,"min_height":1,"ratio":null}`,
		`{"min_width":1,"min_height":1,"RATIO":"any"}`,
		`{"min_width":1,"min_height":1,"ratio":"any","unsupported":true}`,
	} {
		for _, route := range []string{"folders", "searches"} {
			fixture := &searchConfigurationFixture{}
			response := httptest.NewRecorder()
			body := `{"revision":"reviewed","id":"saved","image_rules":` + rule + `}`
			Handler(fixture).ServeHTTP(response, httptest.NewRequest("PATCH", "/v1/configuration/"+route, strings.NewReader(body)))
			if response.Code != 400 || fixture.writes != 0 {
				t.Fatalf("ambiguous nested rules reached writer: %d %s", response.Code, response.Body.String())
			}
		}
	}
	fixture := &configurationFixture{}
	response := httptest.NewRecorder()
	body := `{"revision":"reviewed","path":"/Pictures","image_rules":{"min_width":1,"min_height":1,"ratio":"any","ratio_presets":[]}}`
	Handler(fixture).ServeHTTP(response, httptest.NewRequest("POST", "/v1/configuration/folders", strings.NewReader(body)))
	if response.Code != 200 || fixture.writes != 1 || fixture.edit.ImageRules == nil || fixture.edit.ImageRules.MinWidth != 1 {
		t.Fatalf("valid nested rules lost: %d %s %+v", response.Code, response.Body.String(), fixture.edit)
	}
}
