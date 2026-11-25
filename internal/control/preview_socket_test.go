//go:build linux || darwin

package control

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vadim82/panora/internal/playback"
)

func TestPreviewPrivateSocketRoundTripAndAccessChecks(t *testing.T) {
	path := socketPath(t)
	fixture := &previewFixture{}
	serveFixture(t, path, Handler(fixture))
	got, err := ReadPreview(t.Context(), path, 23)
	if err != nil || !got.Available || got.ImageID != 23 || got.MIMEType != "image/png" || string(got.Data) != "fixture" || fixture.id != 23 || fixture.actions != 0 {
		t.Fatalf("preview=%+v %v", got, err)
	}
	if _, err := ReadPreview(t.Context(), path, 0); err == nil {
		t.Fatal("zero ID accepted")
	}
	if err := os.Chmod(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(path), 0700) })
	_, err = ReadPreview(t.Context(), path, 23)
	if err == nil {
		t.Fatal("preview used a nonprivate socket directory")
	}
}

func TestPreviewSocketClientBoundsResponseAndPreservesTypedErrors(t *testing.T) {
	path := socketPath(t)
	serveFixture(t, path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/1/") {
			_, _ = w.Write(bytes.Repeat([]byte{'x'}, playback.MaxPreviewMessageBytes+1))
			return
		}
		writeAPIError(w, http.StatusNotFound, "preview_not_found", "fixture missing image")
	}))
	if _, err := ReadPreview(context.Background(), path, 1); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("unbounded response accepted=%v", err)
	}
	_, err := ReadPreview(t.Context(), path, 2)
	typed, ok := err.(*Error)
	if !ok || typed.Code != "preview_not_found" {
		t.Fatalf("typed unavailable error=%v", err)
	}
}
