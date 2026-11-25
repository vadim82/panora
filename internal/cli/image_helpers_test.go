package cli

import (
	"crypto/sha256"
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
	im := image.NewRGBA(image.Rect(0, 0, width, height))
	shade := sha256.Sum256([]byte(filepath.Base(path)))
	im.Set(0, 0, color.RGBA{R: shade[0], G: shade[1], B: shade[2], A: 255})
	encodeErr := png.Encode(f, im)
	closeErr := f.Close()
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
}
