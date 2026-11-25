package playback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/source"
)

func TestRulePreviewHeaderBudgetRejectsGrowthAfterPathStat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "growing.png")
	writeScanImage(t, path, 2, 2)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Preserve a valid header but grow the file after the preview reserved bytes
	// using its earlier pathname stat. The reopened descriptor must enforce that
	// reservation, even though DecodeConfig could read the unchanged header.
	if err := os.Truncate(path, before.Size()+1024); err != nil {
		t.Fatal(err)
	}
	if _, err := readImageInfoLimited(t.Context(), path, before.Size()); !errors.Is(err, errImageHeaderBudget) {
		t.Fatalf("a stale stat authorized a larger image read: %v", err)
	}
}

func draftRequest(t *testing.T, service *Service) RulePreviewRequest {
	t.Helper()
	config := settings(t, service)
	return RulePreviewRequest{Revision: config.Revision, Rules: config.Rules}
}

func TestRulePreviewChecksDraftHeadersWithoutSavingOrTakingPlaybackOwnership(t *testing.T) {
	root := t.TempDir()
	writeScanImage(t, filepath.Join(root, "wide.png"), 64, 36)
	writeScanImage(t, filepath.Join(root, "square.png"), 64, 64)
	writeScanImage(t, filepath.Join(root, "small.png"), 32, 18)
	service, _, backend := configurationFixture(t, localTOML(root)+"[filters]\nmin_width=2\nmin_height=2\n")
	refreshForTest(t, service)
	before := settings(t, service)
	state := service.Status()
	request := draftRequest(t, service)
	request.Rules.MinWidth, request.Rules.MinHeight, request.Rules.Ratio = 64, 36, "presets"
	request.Rules.RatioPresets = []string{"16:9"}
	service.actionMu.Lock()
	preview, err := service.PreviewRules(t.Context(), request)
	service.actionMu.Unlock()
	if err != nil || preview.Inspected != 3 || preview.Matched != 1 || preview.Rejected != 2 || preview.Unknown != 0 || preview.Reasons.BelowMinimum != 1 || preview.Reasons.RatioMismatch != 1 || !preview.Complete || preview.Validation != "local_headers_and_validated_cache" {
		t.Fatalf("draft preview=%+v %v", preview, err)
	}
	if !reflect.DeepEqual(service.Status(), state) || settings(t, service).TOML != before.TOML || backend.calls.Load() != 0 {
		t.Fatal("rule preview changed settings, playback or desktop")
	}
	request.Rules.MaxDepth++
	preview, err = service.PreviewRules(t.Context(), request)
	if err != nil || preview.Complete || preview.Reason != "draft_depth_differs" {
		t.Fatalf("draft depth claimed an exhaustive scan=%+v %v", preview, err)
	}
	request.Revision = "stale"
	_, err = service.PreviewRules(t.Context(), request)
	requireConfigError(t, err, "config_conflict")
}

func TestRulePreviewRemoteMetadataAndUnavailableCachedBytesRemainUnknown(t *testing.T) {
	service, _, backend := configurationFixture(t, "version=1\n[[providers]]\nid='wallhaven'\nenabled=true\n[[providers.searches]]\nid='nature'\nquery='landscape'\n[filters]\nmin_width=2\nmin_height=2\n")
	config, _, err := LoadConfig(service.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	row := source.Candidate{ID: "remote", MediaKind: "image", Ref: json.RawMessage(`{"id":"remote"}`), Dimensions: &source.Dimensions{Width: 9999, Height: 9999}, Content: &source.Content{Kind: "https", MediaKind: "image", URL: "https://example.invalid/no-download.png"}}
	identity, err := service.store.EnsureImage(t.Context(), "wallhaven", "nature", config.InputRevision("wallhaven", "nature"), "", row)
	if err != nil {
		t.Fatal(err)
	}
	request := draftRequest(t, service)
	preview, err := service.PreviewRules(t.Context(), request)
	if err != nil || preview.Matched != 0 || preview.Unknown != 1 || preview.Reasons.UnknownRemote != 1 {
		t.Fatalf("remote metadata counted as proven=%+v %v", preview, err)
	}
	original := filepath.Join(t.TempDir(), "local.png")
	writeScanImage(t, original, 2, 2)
	materialized, err := service.cache.Materialize(t.Context(), source.Content{Kind: "local", MediaKind: "image", Path: original})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.store.SaveCachedImage(t.Context(), identity.ImageID, catalog.CachedFile{Path: materialized.Path, Hash: materialized.Hash, Width: materialized.Width, Height: materialized.Height, Bytes: materialized.Bytes}); err != nil {
		t.Fatal(err)
	}
	preview, err = service.PreviewRules(t.Context(), request)
	if err != nil || preview.Matched != 1 || preview.Unknown != 0 {
		t.Fatalf("validated remote bytes not inspected=%+v %v", preview, err)
	}
	if err := os.WriteFile(materialized.Path, bytes.Repeat([]byte{'x'}, int(materialized.Bytes)), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		preview, err = service.PreviewRules(t.Context(), request)
		if err != nil || preview.Matched != 0 || preview.Unknown != 1 || preview.Reasons.UnavailableBytes != 1 {
			t.Fatalf("changed/evicted bytes trusted retained dimensions=%+v %v", preview, err)
		}
		if i == 0 {
			if err := os.Remove(materialized.Path); err != nil {
				t.Fatal(err)
			}
		}
	}
	if backend.calls.Load() != 0 || service.Status().HistoryLength != 0 {
		t.Fatal("rule preview attempted remote recovery or desktop application")
	}
}

func TestRulePreviewFileBudgetAndConcurrentRefreshAreExplicitlyPartial(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "large.png")
	writeScanImage(t, path, 2, 2)
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(RulePreviewFileBytes + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	service, _, _ := configurationFixture(t, localTOML(root)+"[filters]\nmin_width=2\nmin_height=2\n")
	refreshForTest(t, service)
	request := draftRequest(t, service)
	preview, err := service.PreviewRules(t.Context(), request)
	if err != nil || preview.Complete || preview.Reason != "file_budget" || preview.Matched != 0 || preview.Unknown != 1 || preview.Reasons.InspectionBudget != 1 {
		t.Fatalf("file budget not reported=%+v %v", preview, err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	service.request.Source = changedProvider{page: func(page source.Page) source.Page {
		close(entered)
		<-release
		return page
	}}
	service.StartDiscovery()
	<-entered
	preview, err = service.PreviewRules(t.Context(), request)
	if err != nil || preview.Complete {
		t.Errorf("concurrent refresh claimed completeness=%+v %v", preview, err)
	}
	if _, err := service.Action(t.Context(), "pause"); err != nil {
		t.Errorf("preview/refresh blocked playback ownership=%v", err)
	}
	close(release)
}

func TestRulePreviewCancellationAndGeometryDeadlineKeepStatusResponsive(t *testing.T) {
	service, _, _ := configurationFixture(t, "version=1\n")
	request := draftRequest(t, service)
	request.Rules.Ratio = "monitor"
	entered := make(chan struct{})
	service.request.InspectCapabilities = func(ctx context.Context, _ string) (desktop.Capabilities, error) {
		close(entered)
		<-ctx.Done()
		return desktop.Capabilities{}, ctx.Err()
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := service.PreviewRules(ctx, request); done <- err }()
	<-entered
	if _, err := service.Action(t.Context(), "pause"); err != nil {
		t.Fatal("rule preview held mutation lock", err)
	}
	_ = service.Status()
	cancel()
	requireConfigError(t, <-done, "canceled")
	synctest.Test(t, func(t *testing.T) {
		other, _, _ := configurationFixture(t, "version=1\n")
		draft := draftRequest(t, other)
		draft.Rules.Ratio = "monitor"
		other.request.InspectCapabilities = func(ctx context.Context, _ string) (desktop.Capabilities, error) {
			<-ctx.Done()
			return desktop.Capabilities{}, ctx.Err()
		}
		started := time.Now()
		preview, err := other.PreviewRules(t.Context(), draft)
		if err != nil || preview.Complete || preview.Reason != "time_budget" || time.Since(started) != RulePreviewTimeout {
			t.Fatalf("bounded geometry preview=%+v %v elapsed=%s", preview, err, time.Since(started))
		}
	})
}
