package playback

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/source"
)

func TestSaveWallpaperCopiesAcceptedBytesWithoutChangingPlayback(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	s, _ := testService(t, backend)
	accepted := act(t, s, "next")
	running := act(t, s, "resume")
	origin := s.currentOriginPath(t.Context())
	full, err := os.ReadFile(origin)
	if err != nil {
		t.Fatal(err)
	}
	config := settings(t, s)
	folder := t.TempDir()
	reviewed := SaveWallpaperRequest{Instance: accepted.InstanceID, History: accepted.Current.ID, ImageID: accepted.Current.ImageID,
		Revision: config.Revision, Folder: folder}
	collision := filepath.Join(folder, "Panora-"+s.currentHash+".png")
	unrelated := []byte("keep this file")
	if err := os.WriteFile(collision, unrelated, 0600); err != nil {
		t.Fatal(err)
	}
	resolvedFolder, err := filepath.EvalSymlinks(folder)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.SaveWallpaper(t.Context(), reviewed)
	if err != nil || saved.AlreadySaved || filepath.Dir(saved.Path) != resolvedFolder || filepath.Ext(saved.Path) != ".png" || saved.Path == collision {
		t.Fatalf("save=%+v error=%v", saved, err)
	}
	if first, _ := os.ReadFile(saved.Path); !bytes.Equal(first, full) {
		t.Fatal("export bytes differ from the validated original")
	}
	if kept, _ := os.ReadFile(collision); !bytes.Equal(kept, unrelated) {
		t.Fatal("conflicting file was replaced")
	}
	if err := os.Remove(collision); err != nil {
		t.Fatal(err)
	}
	if again, err := s.SaveWallpaper(t.Context(), reviewed); err != nil || !again.AlreadySaved || again.Path != saved.Path {
		t.Fatalf("duplicate=%+v error=%v", again, err)
	}
	if info, _ := os.Stat(origin); info == nil || !info.Mode().IsRegular() {
		t.Fatal("the original disappeared")
	}
	after := s.Status()
	if after.HistoryLength != accepted.HistoryLength || after.Current.ID != accepted.Current.ID ||
		after.Paused || after.NextAt == nil || !after.NextAt.Equal(*running.NextAt) || backend.calls != 1 {
		t.Fatalf("save changed playback or scheduling: %+v", after)
	}
	if configuration := settings(t, s); configuration.SaveFolder != "" || configuration.Revision != config.Revision {
		t.Fatal("file export changed configuration")
	}
	config, err = s.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: config.Revision, SaveFolder: &folder})
	if err != nil || config.SaveFolder != folder || len(config.Folders) != 1 {
		t.Fatalf("save folder preference=%+v error=%v", config, err)
	}
	reviewed.Revision, reviewed.Folder = config.Revision, ""
	if again, err := s.SaveWallpaper(t.Context(), reviewed); err != nil || !again.AlreadySaved {
		t.Fatalf("remembered folder duplicate=%+v error=%v", again, err)
	}
	if err := os.Remove(accepted.Current.Path); err != nil {
		t.Fatal(err)
	}
	if independent, err := os.ReadFile(saved.Path); err != nil || !bytes.Equal(independent, full) {
		t.Fatalf("saved file depends on cache: %v", err)
	}
	_, err = s.SaveWallpaper(t.Context(), reviewed)
	requireConfigError(t, err, "image_unavailable")
}

func TestSaveWallpaperRejectsStaleIdentityAndInvalidFolders(t *testing.T) {
	s, _ := testService(t, &fakeWallpaperBackend{})
	first := act(t, s, "next")
	second := act(t, s, "next")
	config := settings(t, s)
	folder := t.TempDir()
	request := SaveWallpaperRequest{Instance: first.InstanceID, History: first.Current.ID,
		ImageID: first.Current.ImageID, Revision: config.Revision, Folder: folder}
	_, err := s.SaveWallpaper(t.Context(), request)
	requireConfigError(t, err, "current_changed")
	request.History, request.ImageID = second.Current.ID, second.Current.ImageID
	request.Folder = filepath.Join(folder, "missing")
	_, err = s.SaveWallpaper(t.Context(), request)
	requireConfigError(t, err, "save_folder")
	request.Folder = filepath.Join(folder, "cache-link")
	if err := os.Symlink(s.request.CachePath, request.Folder); err != nil {
		t.Fatal(err)
	}
	_, err = s.SaveWallpaper(t.Context(), request)
	requireConfigError(t, err, "save_folder")
	if err := os.Remove(request.Folder); err != nil {
		t.Fatal(err)
	}
	request.Folder, request.Instance = folder, "replaced-service"
	_, err = s.SaveWallpaper(t.Context(), request)
	requireConfigError(t, err, "instance_changed")
	request.Instance = second.InstanceID
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = s.SaveWallpaper(ctx, request)
	requireConfigError(t, err, "save_timeout")
	files, err := os.ReadDir(folder)
	if err != nil || len(files) != 0 {
		t.Fatalf("failed saves left files: %v %v", files, err)
	}
}

func TestSaveWallpaperKeepsWallhavenBytesAfterSearchRemoval(t *testing.T) {
	s, _, _ := configurationFixture(t, `version=1
[filters]
min_width=2
min_height=2
ratio="any"
[[providers]]
id="wallhaven"
enabled=true
[[providers.searches]]
id="nature"
query="nature"
`)
	provider := &resolvingWallhaven{}
	s.request.Providers = map[string]source.Provider{"wallhaven": provider}
	ctx := t.Context()
	cfg, _, err := LoadConfig(s.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := s.store.EnsureImage(ctx, "wallhaven", "nature", cfg.InputRevision("wallhaven", "nature"), "", metadataCandidate("image-one"))
	if err != nil {
		t.Fatal(err)
	}
	var full bytes.Buffer
	if err := jpeg.Encode(&full, image.NewRGBA(image.Rect(0, 0, 48, 27)), nil); err != nil {
		t.Fatal(err)
	}
	sourceFile := filepath.Join(t.TempDir(), "image.jpg")
	if err := os.WriteFile(sourceFile, full.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	materialized, err := s.cache.Materialize(ctx, source.Content{Kind: "local", MediaKind: "image", Path: sourceFile})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.SaveCachedImage(ctx, identity.ImageID, catalog.CachedFile(materialized)); err != nil {
		t.Fatal(err)
	}
	accepted, err := s.Action(ctx, "next")
	if err != nil || accepted.Current == nil || accepted.CurrentImage == nil || accepted.CurrentImage.Provider != "wallhaven" {
		t.Fatalf("fixture did not accept a Wallhaven image: %+v %v", accepted, err)
	}
	reviewed := settings(t, s)
	saved, err := s.SaveWallpaper(ctx, SaveWallpaperRequest{
		Instance: accepted.InstanceID, History: accepted.Current.ID, ImageID: accepted.Current.ImageID,
		Revision: reviewed.Revision, Folder: t.TempDir(),
	})
	if err != nil || filepath.Ext(saved.Path) != ".jpg" || len(provider.resolvedIDs) != 0 {
		t.Fatalf("save=%+v error=%v resolutions=%v", saved, err, provider.resolvedIDs)
	}
	if _, err := s.RemoveSearch(ctx, RemoveSearchRequest{Revision: reviewed.Revision, ID: "nature"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(accepted.Current.Path); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(saved.Path)
	if err != nil || !bytes.Equal(actual, full.Bytes()) {
		t.Fatalf("saved bytes were unavailable after search removal and cache loss: %v", err)
	}
}
