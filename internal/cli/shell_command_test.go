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
	"time"

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
	config, err := control.ReadConfiguration(t.Context(), socket)
	if err != nil || len(config.Folders) != 1 {
		t.Fatalf("could not read fixture folder: %+v, %v", config.Folders, err)
	}
	name := "Favorites"
	if _, err := control.UpdateFolder(t.Context(), socket, playback.UpdateFolderRequest{
		Revision: config.Revision, ID: config.Folders[0].ID, Name: &name,
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		status, err := control.Request(t.Context(), socket, "")
		if err == nil && status.CurrentImage != nil && status.CurrentImage.SourceName == name {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("status did not reflect the saved source name: %+v, %v", status.CurrentImage, err)
		}
		time.Sleep(5 * time.Millisecond)
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
		if preview && (!strings.HasPrefix(snapshot.Image.DataURI, "data:image/png;base64,") || snapshot.Image.PreviewState != "available" || snapshot.Image.Origin != "Local folders" || snapshot.Image.SourceName != name || snapshot.Image.Width != 2) {
			t.Fatalf("preview=%+v", snapshot.Image)
		}
		if !preview && (snapshot.Image.DataURI != "" || snapshot.Image.PreviewState != "not_requested" || snapshot.Image.Title != "Accepted image" || snapshot.Image.SourceName != "" || snapshot.Image.Width != 0) {
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
			got := readShellSnapshot(t.Context(), path, true, false)
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
	got := readShellSnapshot(t.Context(), filepath.Join(t.TempDir(), "missing.sock"), true, false)
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

func TestShellCoverFlowReadsOnlyMatchingCachedNeighbors(t *testing.T) {
	for _, scenario := range []string{"forward", "prepared", "prepared-over-forward", "unavailable", "changed", "failed"} {
		t.Run(scenario, func(t *testing.T) {
			var statusReads, previousReads, nextReads atomic.Int32
			pngData := shellPNG(t)
			path := shellHTTPFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("snapshot made a mutation: %s", r.Method)
				}
				thumbnail := playback.ImagePreview{Version: 1, Available: true, MIMEType: "image/png", Width: 2, Height: 2, Data: pngData}
				switch r.URL.Path {
				case "/v1/status":
					status := shellStatus()
					status.HistoryLength, status.HistoryCursor = 2, 1
					status.PreviousHistory = &playback.HistoryReference{ID: "history-prev", ImageID: 2}
					status.PreparedNextImageID, status.PreparedNextPreviewContent = 3, "prepared-content"
					switch scenario {
					case "forward":
						status.HistoryLength = 3
						status.PreparedNextImageID = 0
						status.ForwardHistory = &playback.HistoryReference{ID: "history-next", ImageID: 3, ContentRevision: "prepared-content"}
					case "prepared-over-forward":
						status.HistoryLength = 3
						status.ForwardHistory = &playback.HistoryReference{ID: "unplayable-forward", ImageID: 4}
					}
					if scenario == "failed" {
						status.LastError = &playback.Error{Code: "apply_failed"}
					}
					if statusReads.Add(1) == 3 && scenario == "changed" {
						status.PreparedNextImageID = 4
					}
					_ = json.NewEncoder(w).Encode(status)
					return
				case "/v1/images/1/preview":
					thumbnail.ImageID = 1
				case "/v1/images/2/preview":
					previousReads.Add(1)
					if r.URL.Query().Get("history") != "history-prev" {
						t.Errorf("previous read lacked history identity: %s", r.URL)
					}
					thumbnail.ImageID = 2
				case "/v1/images/3/preview":
					nextReads.Add(1)
					wantHistory := ""
					if scenario == "forward" {
						wantHistory = "history-next"
					}
					if r.URL.Query().Get("history") != wantHistory {
						t.Errorf("next read has wrong history scope: %s", r.URL)
					}
					thumbnail.ImageID, thumbnail.ContentRevision = 3, "prepared-content"
					if scenario == "unavailable" {
						thumbnail.ContentRevision = "other-content"
					}
				default:
					t.Errorf("unexpected read: %s", r.URL)
				}
				_ = json.NewEncoder(w).Encode(thumbnail)
			})
			closed := readShellSnapshot(t.Context(), path, false, false)
			if closed.FlowRevision == "" || closed.Flow != nil || statusReads.Load() != 1 ||
				previousReads.Load() != 0 || nextReads.Load() != 0 {
				t.Fatalf("status-only poll did not stay small: %+v", closed)
			}
			open := readShellSnapshot(t.Context(), path, true, true)
			if open.Image == nil || open.Image.PreviewState != "available" || statusReads.Load() != 3 {
				t.Fatalf("open popup lost the accepted preview: %+v", open)
			}
			switch scenario {
			case "changed":
				if open.Flow != nil || open.FlowRevision == closed.FlowRevision {
					t.Fatalf("retained obsolete neighbors: %+v", open)
				}
			case "failed":
				if open.Flow == nil || open.Flow.Previous != nil || open.Flow.Next != nil ||
					previousReads.Load() != 0 || nextReads.Load() != 0 {
					t.Fatalf("read neighbors after a failed change: %+v", open)
				}
			default:
				if open.Flow == nil || open.Flow.Previous == nil || open.Flow.Previous.ID != "2" ||
					previousReads.Load() != 1 || nextReads.Load() != 1 {
					t.Fatalf("missing or repeated neighbor reads: %+v", open)
				}
				next := open.Flow.Next
				if scenario == "unavailable" {
					if next != nil {
						t.Fatalf("returned a thumbnail for different bytes: %+v", next)
					}
				} else if next == nil || next.ID != "3" || (next.HistoryID != "") != (scenario == "forward") {
					t.Fatalf("wrong next preview: %+v", next)
				}
			}
		})
	}
}

func TestShellSourceLabels(t *testing.T) {
	for _, test := range []struct {
		image          playback.CurrentImage
		origin, source string
	}{
		{playback.CurrentImage{Provider: "wallhaven", SourceName: "Night skies"}, "Wallhaven", "Night skies"},
		{playback.CurrentImage{Provider: "wallhaven", SourceName: "Wallhaven"}, "Wallhaven", ""},
		{playback.CurrentImage{Provider: "omarchy", SourceName: "Ethereal"}, "Omarchy theme", "Ethereal"},
		{playback.CurrentImage{Provider: "example.plugin", ProviderName: "Example", SourceName: "Group\nOne"}, "Example", "Group One"},
	} {
		var image shellImage
		labelShellImage(&image, &test.image)
		if image.Origin != test.origin || image.SourceName != test.source {
			t.Errorf("source label = %q, %q; want %q, %q", image.Origin, image.SourceName, test.origin, test.source)
		}
	}
}
