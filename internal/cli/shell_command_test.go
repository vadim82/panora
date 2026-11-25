package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/playback"
)

func shellHTTPFixture(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	path := shortSocket(t)
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return path
}

func TestShellSnapshotRealServiceIsReadOnly(t *testing.T) {
	options, socket := serviceFixture(t)
	backend := &fakeWallpaperBackend{}
	launchService(t, options, socket, func(context.Context) (desktop.WallpaperBackend, error) { return backend, nil })
	awaitDiscovery(t, socket)
	instance := cliStatus(t, socket, "status").InstanceID
	_, err := exclusionCLI(t, "next", "--socket", socket, "--instance", "stale-instance")
	var apiErr *control.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "instance_changed" {
		t.Fatalf("stale service action: %v", err)
	}
	if _, err := exclusionCLI(t, "next", "--socket", socket, "--instance", instance); err != nil {
		t.Fatal(err)
	}
	for _, preview := range []bool{false, true} {
		args := []string{"shell", "snapshot", "--socket", socket}
		if preview {
			args = append(args, "--preview")
		}
		raw, err := exclusionCLI(t, args...)
		if err != nil {
			t.Fatal(err)
		}
		var snapshot shellSnapshot
		if err := json.Unmarshal([]byte(raw), &snapshot); err != nil || !snapshot.Connected || snapshot.Image == nil {
			t.Fatalf("snapshot=%s, error=%v", raw, err)
		}
		if preview && (!strings.HasPrefix(snapshot.Image.DataURI, "data:image/png;base64,") || snapshot.Image.PreviewState != "available" || snapshot.Image.Origin != "Local folders" || snapshot.Image.Width != 2) {
			t.Fatalf("preview=%+v", snapshot.Image)
		}
		if !preview && (snapshot.Image.DataURI != "" || snapshot.Image.PreviewState != "not_requested" || snapshot.Image.Title != "Accepted image" || snapshot.Image.Width != 0) {
			t.Fatalf("ordinary polling included image details: %+v", snapshot.Image)
		}
		if strings.Contains(raw, options.Request.ConfigPath) || strings.Contains(raw, "original_path") {
			t.Fatalf("snapshot exposed paths: %s", raw)
		}
	}
	if backend.calls != 1 {
		t.Fatalf("expected one wallpaper change from the matching action: %d calls", backend.calls)
	}
}

func shellStatus() playback.Status {
	return playback.Status{Version: 1, InstanceID: "service-one", Paused: true,
		HistoryLength: 1, HistoryCursor: 0,
		Current:      &playback.Entry{ID: "history-one", ImageID: 1},
		CurrentImage: &playback.CurrentImage{Provider: "local", OriginalPath: "/secret/<img src=tracker>.png"}}
}

func shellPNG(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestShellSnapshotRejectsStaleOrMalformedPreviews(t *testing.T) {
	for _, name := range []string{"wrong-image", "wrong-mime", "wrong-size", "oversized", "corrupt", "new-instance", "new-image", "disconnected-after-preview"} {
		t.Run(name, func(t *testing.T) {
			var reads atomic.Int32
			thumbnail := playback.ImagePreview{Version: 1, ImageID: 1, Available: true, MIMEType: "image/png", Width: 2, Height: 2, Data: shellPNG(t)}
			switch name {
			case "wrong-image":
				thumbnail.ImageID = 2
			case "wrong-mime":
				thumbnail.MIMEType = "image/svg+xml"
			case "wrong-size":
				thumbnail.Width = 100000
			case "oversized":
				thumbnail.Data = make([]byte, playback.MaxPreviewBytes+1)
			case "corrupt":
				thumbnail.Data = []byte("not a PNG")
			}
			path := shellHTTPFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("snapshot made a mutation: %s %s", r.Method, r.URL.Path)
				}
				switch r.URL.Path {
				case "/v1/status":
					status := shellStatus()
					if reads.Add(1) > 1 {
						switch name {
						case "new-instance":
							status.InstanceID = "service-two"
						case "new-image":
							status.Current.ID, status.Current.ImageID = "history-two", 2
						case "disconnected-after-preview":
							w.WriteHeader(http.StatusServiceUnavailable)
							_, _ = w.Write([]byte(`{"version":1,"error":{"code":"unavailable","message":"private diagnostic"}}`))
							return
						}
					}
					_ = json.NewEncoder(w).Encode(status)
				case "/v1/images/1/preview":
					_ = json.NewEncoder(w).Encode(thumbnail)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
				}
			})
			got := readShellSnapshot(t.Context(), path, true)
			if reads.Load() < 2 {
				t.Fatalf("fixture did not exercise status/preview reconciliation: %+v", got)
			}
			if got.Image != nil && got.Image.DataURI != "" {
				t.Fatal("unsafe or stale preview was retained")
			}
			if name == "disconnected-after-preview" && (got.Connected || got.Image != nil) {
				t.Fatalf("disconnection retained a current preview: %+v", got)
			}
			if name == "new-instance" && got.InstanceID != "service-two" {
				t.Fatal("new instance was not reconciled")
			}
			if name == "new-image" && (got.Image == nil || got.Image.ID != "2") {
				t.Fatal("new current image was not reconciled")
			}
			if (name == "new-instance" || name == "new-image") && (got.Image == nil || got.Image.Title != "Accepted image") {
				t.Fatalf("reconciliation retained obsolete image labels: %+v", got.Image)
			}
		})
	}
}

func TestShellDisconnectedAndUntrustedLabels(t *testing.T) {
	got := readShellSnapshot(t.Context(), filepath.Join(t.TempDir(), "missing.sock"), true)
	if got.Connected || got.Code != "service_unavailable" || got.Version != 1 {
		t.Fatalf("missing service=%+v", got)
	}
	if got := shellText("<img src=tracker>\x00\n"+strings.Repeat("界", 300), 256); len([]rune(got)) != 256 || strings.ContainsAny(got, "\x00\n") {
		t.Fatal("untrusted label was not bounded/cleaned")
	}
	status := shellStatus()
	status.Version = 99
	if projectShellStatus(status).Connected {
		t.Fatal("unsupported status protocol accepted")
	}
}
