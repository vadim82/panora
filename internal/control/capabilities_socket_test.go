//go:build linux || darwin

package control

import (
	"context"
	"testing"
)

func TestCapabilitiesPrivateSocketRoundTrip(t *testing.T) {
	path := socketPath(t)
	serveFixture(t, path, Handler(&capabilityFixture{}))
	got, err := ReadCapabilities(t.Context(), path)
	if err != nil || got.Version != APIVersion || got.State != "partial" {
		t.Fatalf("capabilities=%+v %v", got, err)
	}
}

func TestCapabilitiesPrivateSocketPreservesTypedTimeout(t *testing.T) {
	path := socketPath(t)
	serveFixture(t, path, Handler(&capabilityFixture{err: context.DeadlineExceeded}))
	_, err := ReadCapabilities(t.Context(), path)
	typed, ok := err.(*Error)
	if !ok || typed.Code != "timeout" {
		t.Fatalf("timeout code lost=%v", err)
	}
}
