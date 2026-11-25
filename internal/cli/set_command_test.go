package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/vadim82/panora/internal/desktop"
)

type fakeWallpaperBackend struct {
	calls   int
	ctx     context.Context
	path    string
	options desktop.DisplayOptions
	err     error
}

func (*fakeWallpaperBackend) Probe(context.Context) error { return nil }

func (b *fakeWallpaperBackend) SetWallpaper(ctx context.Context, path string, options desktop.DisplayOptions) error {
	b.calls++
	b.ctx = ctx
	b.path = path
	b.options = options
	return b.err
}

func TestSetCommandInjectedBackend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wallpaper $(literal).png")
	writeScanImage(t, path, 2, 2)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relativePath, err := filepath.Rel(cwd, path)
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeWallpaperBackend{}
	cmd := newSetCommand(func(context.Context) (desktop.WallpaperBackend, error) {
		return backend, nil
	})
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{relativePath})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatal(err)
	}
	if backend.calls != 1 || backend.path != path || backend.ctx != ctx {
		t.Fatalf("backend got calls=%d path=%q context=%v; want one call with %q and command context", backend.calls, backend.path, backend.ctx, path)
	}
	if want := "Wallpaper request accepted: " + path + "\n"; output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}

func TestSetCommandBackendError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wallpaper.png")
	writeScanImage(t, path, 2, 2)
	wantErr := errors.New("backend unavailable")
	backend := &fakeWallpaperBackend{err: wantErr}
	cmd := newSetCommand(func(context.Context) (desktop.WallpaperBackend, error) {
		return backend, nil
	})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{path})
	if err := cmd.Execute(); !errors.Is(err, wantErr) {
		t.Fatalf("got %v, want backend error %v", err, wantErr)
	}
	if backend.calls != 1 {
		t.Fatalf("backend calls = %d, want 1", backend.calls)
	}
	if output.Len() != 0 {
		t.Fatalf("unexpected success output: %q", output.String())
	}
}

func TestSetCommandRejectsInvalidInputBeforeBackend(t *testing.T) {
	dir := t.TempDir()
	corrupt := filepath.Join(dir, "corrupt.png")
	if err := os.WriteFile(corrupt, []byte("not an image"), 0600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		args []string
	}{
		{"no arguments", []string{}},
		{"extra arguments", []string{corrupt, corrupt}},
		{"directory", []string{dir}},
		{"corrupt image", []string{corrupt}},
		{"missing image", []string{filepath.Join(dir, "missing.png")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newSetCommand(func(context.Context) (desktop.WallpaperBackend, error) {
				t.Fatal("backend resolution called for invalid input")
				return nil, nil
			})
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true
			cmd.SetArgs(tt.args)
			if err := cmd.Execute(); err == nil {
				t.Fatal("expected input error")
			}
		})
	}
}

func TestSetCommandDisplayOptions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.png")
	writeScanImage(t, path, 2, 2)
	for _, tt := range []struct {
		name    string
		flags   []string
		want    desktop.DisplayOptions
		invalid bool
	}{
		{"default", nil, desktop.DisplayOptions{Fit: "auto"}, false},
		{"contain", []string{"--fit", "contain"}, desktop.DisplayOptions{Fit: "contain"}, false},
		{"upscale", []string{"--upscale"}, desktop.DisplayOptions{Fit: "auto", Upscale: true}, false},
		{"crop", []string{"--fit", "crop"}, desktop.DisplayOptions{Fit: "crop"}, false},
		{"unknown", []string{"--fit", "stretch"}, desktop.DisplayOptions{}, true},
		{"contradictory", []string{"--fit", "crop", "--upscale"}, desktop.DisplayOptions{}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			backend := &fakeWallpaperBackend{}
			cmd := newSetCommand(func(context.Context) (desktop.WallpaperBackend, error) {
				if tt.invalid {
					t.Fatal("invalid options triggered resolution")
				}
				return backend, nil
			})
			cmd.SetOut(new(bytes.Buffer))
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true
			cmd.SetArgs(append([]string{path}, tt.flags...))
			err := cmd.Execute()
			if tt.invalid {
				if err == nil || backend.calls != 0 {
					t.Fatalf("error=%v, calls=%d", err, backend.calls)
				}
			} else if err != nil || backend.options != tt.want {
				t.Fatalf("error=%v, options=%+v, want %+v", err, backend.options, tt.want)
			}
		})
	}
}
