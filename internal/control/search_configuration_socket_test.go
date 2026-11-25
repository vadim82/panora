//go:build linux || darwin

package control

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/vadim82/panora/internal/playback"
)

func TestSearchMutationUncertainResponseRequiresSettingsReloadWithoutReplay(t *testing.T) {
	for _, response := range []string{"lost", "invalid JSON"} {
		for _, method := range []string{http.MethodPost, http.MethodPatch, http.MethodDelete} {
			t.Run(response+"/"+method, func(t *testing.T) {
				path := socketPath(t)
				var calls atomic.Int32
				serveFixture(t, path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					if r.Method != method || r.URL.Path != "/v1/configuration/searches" {
						t.Errorf("unexpected request=%s %s", r.Method, r.URL.Path)
					}
					if response == "invalid JSON" {
						_, _ = w.Write([]byte("{"))
						return
					}
					connection, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = connection.Close()
				}))
				var err error
				query := "stars"
				switch method {
				case http.MethodPost:
					_, err = AddSearch(t.Context(), path, playback.AddSearchRequest{Revision: "reviewed", Query: "nature"})
				case http.MethodPatch:
					_, err = UpdateSearch(t.Context(), path, playback.UpdateSearchRequest{Revision: "reviewed", ID: "saved", Query: &query})
				case http.MethodDelete:
					_, err = RemoveSearch(t.Context(), path, playback.RemoveSearchRequest{Revision: "reviewed", ID: "saved"})
				}
				if err == nil || !strings.Contains(err.Error(), "reload settings before retrying") || strings.Contains(err.Error(), "panora status") || calls.Load() != 1 {
					t.Fatalf("recovery guidance/replay: calls=%d err=%v", calls.Load(), err)
				}
				if response == "lost" && !strings.Contains(err.Error(), "may have completed") {
					t.Fatalf("uncertain outcome unexplained=%v", err)
				}
			})
		}
	}
}
