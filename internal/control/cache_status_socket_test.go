//go:build linux || darwin

package control

import "testing"

func TestCacheStatusThroughPrivateSocket(t *testing.T) {
	path := socketPath(t)
	serveFixture(t, path, Handler(&cacheFixture{}))
	got, err := ReadCacheStatus(t.Context(), path)
	if err != nil || got.Version != APIVersion || got.Saved.MaxImages != 1 || got.Saved != got.Effective || got.Usage.State != "protected_over_budget" || got.Usage.ProtectedImages != 2 {
		t.Fatalf("cache status=%+v %v", got, err)
	}
}
