package playback

import (
	"crypto/sha256"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func writeScanImage(t *testing.T, path string, width, height int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	digest := sha256.Sum256([]byte(filepath.Base(path)))
	img.Set(0, 0, color.RGBA{R: digest[0], G: digest[1], B: digest[2], A: 255})
	encodeErr := png.Encode(f, img)
	closeErr := f.Close()
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
}

func TestScanDirectoryResults(t *testing.T) {
	root := t.TempDir()
	writeScanImage(t, filepath.Join(root, "a-pass.png"), 32, 9)
	if err := os.WriteFile(filepath.Join(root, "b-corrupt.png"), []byte("not an image"), 0644); err != nil {
		t.Fatal(err)
	}
	writeScanImage(t, filepath.Join(root, "c-reject.png"), 8, 8)
	writeScanImage(t, filepath.Join(root, "child", "included.png"), 32, 9)
	writeScanImage(t, filepath.Join(root, "child", "deep", "excluded.png"), 32, 9)
	count := 0
	ratio, err := ParseRatioRule("32:9", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = ScanDirectoryContext(t.Context(), root, 1, 32, 9, ratio, func(result ScanResult) error {
		count++
		switch filepath.Base(result.Image.Path) {
		case "a-pass.png", "included.png":
			if result.Err != nil || len(result.Reasons) != 0 || result.Image.Width != 32 {
				t.Errorf("unexpected eligible result: %+v", result)
			}
		case "b-corrupt.png":
			if result.Err == nil {
				t.Error("expected decode error")
			}
		case "c-reject.png":
			if result.Err != nil || len(result.Reasons) != 2 {
				t.Errorf("expected size and ratio rejection: %+v", result)
			}
		default:
			t.Errorf("unexpected file: %s", result.Image.Path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 4 {
		t.Errorf("got %d callbacks, want 4", count)
	}
}

func TestScanDirectoryCallbackStopsScan(t *testing.T) {
	root := t.TempDir()
	writeScanImage(t, filepath.Join(root, "a.png"), 32, 9)
	writeScanImage(t, filepath.Join(root, "b.png"), 32, 9)
	stop := errors.New("caller stopped")
	count := 0
	err := ScanDirectoryContext(t.Context(), root, 0, 1, 1, nil, func(result ScanResult) error {
		count++
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatalf("got %v, want callback error", err)
	}
	if count != 1 {
		t.Errorf("got %d callbacks, want 1", count)
	}
}
