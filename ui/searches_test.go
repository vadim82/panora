package main

import (
	"encoding/json"
	"github.com/vadim82/panora/internal/playback"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNativeSearchMutationsUseReviewedRevisionAndNeverReplayLostResponses(t *testing.T) {
	var requests atomic.Int32
	var lose atomic.Bool
	path := serveFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/v1/configuration/searches" {
			t.Errorf("unexpected endpoint=%s", r.URL.Path)
		}
		var edit map[string]any
		if err := json.NewDecoder(r.Body).Decode(&edit); err != nil {
			t.Error(err)
		}
		if edit["revision"] != "reviewed" {
			t.Errorf("edit=%+v", edit)
		}
		if _, ok := edit["purity"]; ok {
			t.Error("client overrode fixed SFW policy")
		}
		if r.Method == http.MethodPatch && edit["enabled"] != false {
			t.Errorf("disabled edit lost explicit false: %+v", edit)
		}
		if lose.Load() {
			connection, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = connection.Close()
			}
			return
		}
		_ = json.NewEncoder(w).Encode(playback.Configuration{Version: 1, Revision: "saved"})
	}))
	client := &Client{ctx: t.Context(), socket: path}
	enabled := false
	add := playback.AddSearchRequest{Revision: "reviewed", Name: "Quiet landscapes", Query: "nature"}
	if got := client.AddSearch(add); got.Configuration == nil {
		t.Fatalf("add=%+v", got)
	}
	if got := client.UpdateSearch(playback.UpdateSearchRequest{Revision: "reviewed", ID: "search-a", Enabled: &enabled}); got.Configuration == nil {
		t.Fatalf("update=%+v", got)
	}
	if got := client.RemoveSearch(playback.RemoveSearchRequest{Revision: "reviewed", ID: "search-a"}); got.Configuration == nil {
		t.Fatalf("remove=%+v", got)
	}
	lose.Store(true)
	if got := client.AddSearch(add); got.Configuration != nil || !strings.Contains(got.Error, "reload settings before retrying") || requests.Load() != 4 {
		t.Fatalf("lost=%+v requests=%d", got, requests.Load())
	}
	client.action.Lock()
	if got := client.AddSearch(add); got.Code != "busy" || requests.Load() != 4 {
		t.Fatalf("concurrent mutation=%+v", got)
	}
	client.action.Unlock()
}
