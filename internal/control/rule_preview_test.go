package control

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vadim82/panora/internal/playback"
)

type rulePreviewFixture struct {
	testService
	reads   int
	request playback.RulePreviewRequest
}

func (f *rulePreviewFixture) PreviewRules(_ context.Context, request playback.RulePreviewRequest) (playback.RulePreview, error) {
	f.reads++
	f.request = request
	return playback.RulePreview{Version: APIVersion, Rules: request.Rules, Revision: request.Revision, Scope: "enabled_catalog", Inspected: 3, Matched: 1, Rejected: 1, Unknown: 1, Complete: true}, nil
}

func TestRulePreviewRouteValidatesNestedDraftWithoutMutations(t *testing.T) {
	for _, test := range []struct {
		method, path, body string
		status, reads      int
	}{
		{"POST", "/v1/rules/preview", `{"revision":"reviewed","rules":{"min_width":1920,"min_height":1080,"max_depth":2,"ratio":"presets","ratio_presets":["16:9"]}}`, 200, 1},
		{"POST", "/v1/rules/preview", `{"revision":"reviewed","rules":{"ratio":"any","ratio":"monitor"}}`, 400, 0},
		{"POST", "/v1/rules/preview", `{"revision":"reviewed","rules":{"min_width":null}}`, 400, 0},
		{"POST", "/v1/rules/preview", `{"revision":"reviewed","rules":null}`, 400, 0},
		{"POST", "/v1/rules/preview", `{"revision":"reviewed","rules":{"unknown":true}}`, 400, 0},
		{"POST", "/v1/rules/preview", `{"revision":"reviewed","rules":{"min_width":"wide"}}`, 400, 0},
		{"POST", "/v1/rules/preview", strings.Repeat(" ", 16385), 413, 0},
		{"GET", "/v1/rules/preview", "", 405, 0},
		{"POST", "/v1/rules/preview?x=1", `{}`, 400, 0},
	} {
		fixture := &rulePreviewFixture{}
		response := httptest.NewRecorder()
		Handler(fixture).ServeHTTP(response, httptest.NewRequest(test.method, test.path, strings.NewReader(test.body)))
		if response.Code != test.status || fixture.reads != test.reads {
			t.Fatalf("response=%d %s reads=%d", response.Code, response.Body.String(), fixture.reads)
		}
	}
}

func TestSourceRulePreviewRoutePreservesInputIdentity(t *testing.T) {
	fixture := &rulePreviewFixture{}
	response := httptest.NewRecorder()
	body := `{"revision":"reviewed","provider_id":"wallhaven","input_id":"saved","rules":{"min_width":1,"min_height":1,"ratio":"any","ratio_presets":[],"max_depth":0}}`
	Handler(fixture).ServeHTTP(response, httptest.NewRequest("POST", "/v1/rules/preview", strings.NewReader(body)))
	if response.Code != 200 || fixture.reads != 1 || fixture.request.ProviderID != "wallhaven" || fixture.request.InputID != "saved" {
		t.Fatalf("source identity lost: %d %s %+v", response.Code, response.Body.String(), fixture.request)
	}
	for _, invalid := range []string{
		`{"revision":"r","provider_id":"local","rules":{"ratio":"any"}}`,
		`{"revision":"r","provider_id":"local","input_id":null,"rules":{"ratio":"any"}}`,
		`{"revision":"r","provider_id":"local","input_id":"one","input_id":"two","rules":{"ratio":"any"}}`,
	} {
		response = httptest.NewRecorder()
		Handler(fixture).ServeHTTP(response, httptest.NewRequest("POST", "/v1/rules/preview", strings.NewReader(invalid)))
		if response.Code != 400 || fixture.reads != 1 {
			t.Fatalf("invalid source identity reached service: %d %s", response.Code, response.Body.String())
		}
	}
}
