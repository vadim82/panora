package localfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRootLinkDepthAndContainment(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(root, "nested", "image.png")
	if err := os.WriteFile(image, nil, 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "root")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	var paths []string
	if err := Walk(context.Background(), alias, 1, func(path string) error { paths = append(paths, path); return nil }); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(alias, "nested", "image.png")
	if len(paths) != 1 || paths[0] != want {
		t.Fatalf("root link lost configured identity: %v", paths)
	}
	if _, err := ContainedPath(alias, want, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := ContainedPath(alias, want, 0); err == nil {
		t.Fatal("ignored scan depth")
	}
	outside := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outside, nil, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escape.png")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ContainedPath(root, link, 1); err == nil {
		t.Fatal("accepted escaping file symlink")
	}
	stop := errors.New("stop")
	if err := Walk(context.Background(), root, 1, func(string) error { return stop }); !errors.Is(err, stop) {
		t.Fatal(err)
	}
}
