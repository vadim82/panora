package playback

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/source"
)

func TestSourceRuleUpgradeInheritanceAndNewInputDefaults(t *testing.T) {
	root := t.TempDir()
	body := localTOML(root) + "[filters]\nmin_width=1920\nmin_height=1080\nratio='16:9'\n"
	service, path, backend := configurationFixture(t, body)
	before := settings(t, service)
	if before.Folders[0].ImageRules != nil || before.Folders[0].EffectiveRules.MinWidth != 1920 {
		t.Fatalf("old source lost inherited rules: %+v", before.Folders[0])
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != body {
		t.Fatal("reading old source rewrote profile")
	}
	added, err := service.AddFolder(t.Context(), AddFolderRequest{Revision: before.Revision, Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if added.Folders[0].ImageRules != nil || added.Folders[0].EffectiveRules.MinWidth != 1920 || added.Folders[1].ImageRules == nil || added.Folders[1].EffectiveRules.MinWidth != 1 || added.Folders[1].EffectiveRules.Ratio != "any" {
		t.Fatalf("upgrade/new rules=%+v", added.Folders)
	}
	inherited, err := service.AddSearch(t.Context(), AddSearchRequest{Revision: added.Revision, Query: "trees", InheritRules: configValue(true)})
	if err != nil || inherited.Searches[0].ImageRules != nil || inherited.Searches[0].EffectiveRules.Ratio != "16:9" {
		t.Fatalf("explicit add inheritance=%+v %v", inherited.Searches, err)
	}
	own := &SourceImageRules{MinWidth: 3, MinHeight: 2, Ratio: "3:2", RatioPresets: []string{}}
	edited, err := service.UpdateFolder(t.Context(), UpdateFolderRequest{Revision: inherited.Revision, ID: "home", ImageRules: own})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*edited.Folders[0].ImageRules, *own) || backend.calls.Load() != 0 || !service.Status().Paused {
		t.Fatal("source save changed playback or rules")
	}
	restored, err := service.UpdateFolder(t.Context(), UpdateFolderRequest{Revision: edited.Revision, ID: "home", InheritRules: configValue(true)})
	if err != nil || restored.Folders[0].ImageRules != nil || restored.Folders[0].EffectiveRules.MinWidth != 1920 {
		t.Fatalf("inherit reset=%+v %v", restored.Folders, err)
	}
	for _, edit := range []UpdateFolderRequest{
		{ImageRules: &SourceImageRules{MinWidth: 0, MinHeight: 1, Ratio: "any"}},
		{ImageRules: own, InheritRules: configValue(true)}, {InheritRules: configValue(false)},
	} {
		edit.Revision, edit.ID = restored.Revision, "home"
		_, err := service.UpdateFolder(t.Context(), edit)
		requireConfigError(t, err, "invalid_settings")
		if settings(t, service).TOML != restored.TOML {
			t.Fatal("invalid source rules changed saved settings")
		}
	}
	_, err = service.UpdateFolder(t.Context(), UpdateFolderRequest{Revision: before.Revision, ID: "home", ImageRules: own})
	requireConfigError(t, err, "config_conflict")
	service.Close()
	restarted := openTestService(t, service.request, service.resolve, service.interval, false)
	after := settings(t, restarted)
	if after.Folders[0].ImageRules != nil || after.Folders[0].EffectiveRules.MinWidth != 1920 || after.Folders[1].EffectiveRules.MinWidth != 1 {
		t.Fatal("source rules did not survive restart")
	}
}

func TestMixedSourceEligibilityAndUnresolvedSourceFallback(t *testing.T) {
	wide, square := t.TempDir(), t.TempDir()
	writeScanImage(t, filepath.Join(wide, "wide.png"), 64, 36)
	writeScanImage(t, filepath.Join(square, "square.png"), 64, 64)
	body := localTOML(wide) + "[providers.folders.image_rules]\nmin_width=1\nmin_height=1\nratio='monitor'\n" + fmt.Sprintf("[[providers.folders]]\nid='square'\npath=%q\n[providers.folders.image_rules]\nmin_width=1\nmin_height=1\nratio='1:1'\n[filters]\nmin_width=9999\nratio='monitor'\n", square)
	service, _, backend := configurationFixture(t, body)
	service.request.InspectCapabilities = func(context.Context, string) (desktop.Capabilities, error) {
		return desktop.Capabilities{}, fmt.Errorf("no geometry")
	}
	refreshForTest(t, service)
	config, _, err := LoadConfig(service.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	pools, err := service.candidatePools(t.Context(), config, maxCatalogCandidates, func(string) {})
	if err != nil || len(pools) != 1 || len(pools[0].inputs) != 1 || len(pools[0].inputs[0]) != 1 || pools[0].inputs[0][0].Identity.InputID != "square" {
		t.Fatalf("unresolved source blocked usable source: %+v %v", pools, err)
	}
	status, err := service.Action(t.Context(), "next")
	if err != nil || status.Current.SourceID != "square" || backend.calls.Load() != 1 {
		t.Fatalf("source selection=%+v %v", status, err)
	}
	saved := settings(t, service)
	_, err = service.UpdateFolder(t.Context(), UpdateFolderRequest{Revision: saved.Revision, ID: "square", ImageRules: &SourceImageRules{MinWidth: 100, MinHeight: 100, Ratio: "any"}})
	if err != nil {
		t.Fatal(err)
	}
	page, err := service.History(t.Context(), "", 10)
	if err != nil || page.Entries[0].Reason != "filter_mismatch" {
		t.Fatalf("history ignored source rules: %+v %v", page, err)
	}
	_, err = service.ReplayHistory(t.Context(), status.Current.ID)
	requireConfigError(t, err, "history_unavailable")
	if backend.calls.Load() != 1 || service.Status().Current.ID != status.Current.ID {
		t.Fatal("failed source replay changed accepted wallpaper")
	}
}

func TestLocalExactResolutionOverridesMinimumBeforeMaterialization(t *testing.T) {
	for _, mode := range []string{rotationBalanceSources, rotationShuffleAllImages} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			writeScanImage(t, filepath.Join(root, "exact.png"), 4, 2)
			body := localTOML(root) + `[providers.folders.image_rules]
min_width=8
min_height=4
exact_resolutions=["4x2"]
ratio="2:1"
` + fmt.Sprintf("\n[playback]\nrotation_mode=%q\n", mode)
			service, _, _ := configurationFixture(t, body)
			refreshForTest(t, service)
			status, err := service.Action(t.Context(), "next")
			if err != nil || status.Current == nil || status.Current.SourceID != "home" {
				t.Fatalf("exact resolution did not play: %+v %v", status, err)
			}
		})
	}
}

func TestSourcePreviewIsolationAndUnenrolledDraft(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	writeScanImage(t, filepath.Join(first, "wide.png"), 64, 36)
	writeScanImage(t, filepath.Join(second, "square.png"), 64, 64)
	service, _, backend := configurationFixture(t, localTOML(first)+fmt.Sprintf("[[providers.folders]]\nid='second'\npath=%q\n", second))
	refreshForTest(t, service)
	before := settings(t, service)
	request := RulePreviewRequest{Revision: before.Revision, ProviderID: "local", InputID: "home", Rules: ImageRules{MinWidth: 1, MinHeight: 1, MaxDepth: before.Folders[0].EffectiveMaxDepth, Ratio: "16:9", RatioPresets: []string{}}}
	preview, err := service.PreviewRules(t.Context(), request)
	if err != nil || preview.Scope != "source_catalog" || preview.ProviderID != "local" || preview.InputID != "home" || preview.Inspected != 1 || preview.Matched != 1 {
		t.Fatalf("preview leaked another source: %+v %v", preview, err)
	}
	request.InputID = "second"
	preview, err = service.PreviewRules(t.Context(), request)
	if err != nil || preview.Inspected != 1 || preview.Rejected != 1 || preview.Matched != 0 {
		t.Fatalf("source preview used another membership: %+v %v", preview, err)
	}
	request.InputID = "unsaved"
	preview, err = service.PreviewRules(t.Context(), request)
	if err != nil || preview.Complete || preview.Inspected != 0 || preview.Reason != "source_not_enabled_or_enrolled" {
		t.Fatalf("unsaved preview claimed catalog knowledge: %+v %v", preview, err)
	}
	if settings(t, service).TOML != before.TOML || backend.calls.Load() != 0 || service.Status().HistoryLength != 0 {
		t.Fatal("source preview changed settings or playback")
	}
	request.InputID = ""
	_, err = service.PreviewRules(t.Context(), request)
	requireConfigError(t, err, "invalid_request")
}

func TestOverlappingSearchMembershipUsesItsOwnRulesAndSharedBytes(t *testing.T) {
	body := "version=1\n[[providers]]\nid='wallhaven'\nenabled=true\n[[providers.searches]]\nid='wide'\nquery='wide'\n[providers.searches.image_rules]\nmin_width=1\nmin_height=1\nratio='16:9'\n[[providers.searches]]\nid='square'\nquery='square'\n[providers.searches.image_rules]\nmin_width=1\nmin_height=1\nratio='1:1'\n"
	service, _, backend := configurationFixture(t, body)
	config, _, err := LoadConfig(service.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	candidate := source.Candidate{ID: "shared", MediaKind: "image", Ref: json.RawMessage(`{"id":"shared"}`), Content: &source.Content{Kind: "https", MediaKind: "image", URL: "https://example.invalid/shared.png"}}
	ids := make([]catalog.Identity, 2)
	for i, input := range []string{"wide", "square"} {
		ids[i], err = service.store.EnsureImage(t.Context(), "wallhaven", input, config.InputRevision("wallhaven", input), "", candidate)
		if err != nil {
			t.Fatal(err)
		}
	}
	if ids[0].ImageID != ids[1].ImageID {
		t.Fatal("overlap lost shared image identity")
	}
	original := filepath.Join(t.TempDir(), "square.png")
	writeScanImage(t, original, 64, 64)
	cached, err := service.cache.Materialize(t.Context(), source.Content{Kind: "local", MediaKind: "image", Path: original})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.store.SaveCachedImage(t.Context(), ids[0].ImageID, catalog.CachedFile{Path: cached.Path, Hash: cached.Hash, Width: cached.Width, Height: cached.Height, Bytes: cached.Bytes}); err != nil {
		t.Fatal(err)
	}
	// Rejected membership must not borrow the square search's eligibility.
	if _, err := service.materializeChoice(t.Context(), playbackChoice{identity: ids[0], historyIndex: -1}, config); err == nil {
		t.Fatal("wide membership borrowed square rules")
	}
	choice, err := service.materializeChoice(t.Context(), playbackChoice{identity: ids[1], historyIndex: -1}, config)
	if err != nil || choice.path != cached.Path || choice.identity.InputID != "square" {
		t.Fatalf("valid overlap could not reuse shared bytes: %+v %v", choice, err)
	}
	service.draw = func(int) int { return 0 }
	selected, selectionErr := service.selectFresh(t.Context(), config, func(string) {})
	if selectionErr != nil || selected.identity.InputID != "square" || backend.calls.Load() != 0 {
		t.Fatalf("overlap selection=%+v %v", selected, selectionErr)
	}
	selected.sourceID = "square"
	if err := service.applyChoice(t.Context(), selected); err != nil {
		t.Fatal(err)
	}
	service.publishStatus()
	// A rejecting membership is eligibility information, not a provider outage:
	// it cannot block the valid current-image repeat through another membership.
	repeated, repeatErr := service.selectFresh(t.Context(), config, func(string) {})
	if repeatErr != nil || repeated.identity.InputID != "square" || repeated.path != cached.Path {
		t.Fatalf("rejected overlap blocked healthy repeat: %+v %v", repeated, repeatErr)
	}
	for _, input := range []string{"wide", "square"} {
		preview, err := service.PreviewRules(t.Context(), RulePreviewRequest{Revision: settings(t, service).Revision, ProviderID: "wallhaven", InputID: input, Rules: ImageRules{MinWidth: 1, MinHeight: 1, Ratio: "1:1"}})
		if err != nil || preview.Inspected != 1 || preview.Matched != 1 {
			t.Fatalf("overlap preview=%+v %v", preview, err)
		}
	}
}

type ruleHintProvider struct {
	offlineReviewProvider
	supported []string
	filters   []*source.FilterHints
}

func (p *ruleHintProvider) Describe(ctx context.Context) (source.Description, error) {
	d, err := p.offlineReviewProvider.Describe(ctx)
	d.FilterHints = p.supported
	return d, err
}
func (p *ruleHintProvider) Discover(_ context.Context, params source.DiscoverParams) (source.Page, error) {
	p.filters = append(p.filters, params.Filter)
	return source.Page{Candidates: []source.Candidate{}, NextCursor: json.RawMessage("null")}, nil
}

func TestSourceDiscoveryHintsRespectCapabilitiesAndInvalidateOldCursor(t *testing.T) {
	service, _, _ := configurationFixture(t, "version=1\n[[providers]]\nid='wallhaven'\nenabled=true\n[[providers.searches]]\nid='nature'\nquery='nature'\n")
	provider := &ruleHintProvider{supported: []string{"min_width", "ratio"}}
	service.request.Providers = map[string]source.Provider{"wallhaven": provider}
	config, _, err := LoadConfig(service.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	oldRevision := config.InputRevision("wallhaven", "nature")
	_, err = service.store.StartScan(t.Context(), "wallhaven", "nature", oldRevision, false)
	if err != nil {
		t.Fatal(err)
	}
	before := settings(t, service)
	saved, err := service.UpdateSearch(t.Context(), UpdateSearchRequest{Revision: before.Revision, ID: "nature", ImageRules: &SourceImageRules{MinWidth: 1920, MinHeight: 1080, Ratio: "16:9"}})
	if err != nil {
		t.Fatal(err)
	}
	config, _, err = LoadConfig(service.request.ConfigPath)
	if err != nil || config.InputRevision("wallhaven", "nature") == oldRevision {
		t.Fatal("changed effective discovery hints reused cursor revision")
	}
	p := config.Providers[0]
	if _, err := service.refreshInputCounts(t.Context(), p, providerInputs(p, config.Filters.MaxDepth, sourceDefaultRules(config))[0], 10); err != nil {
		t.Fatal(err)
	}
	if len(provider.filters) != 1 || provider.filters[0].MinWidth != 1920 || provider.filters[0].MinHeight != 0 || provider.filters[0].Ratio == nil || provider.filters[0].Ratio.Width != 16 {
		t.Fatalf("unsupported field pushed or source hints missing: %+v", provider.filters)
	}
	preview, err := service.PreviewRules(t.Context(), RulePreviewRequest{Revision: saved.Revision, ProviderID: "wallhaven", InputID: "nature", Rules: ImageRules{MinWidth: 1, MinHeight: 1, Ratio: "any"}})
	if err != nil || preview.Inspected != 0 || len(provider.filters) != 1 {
		t.Fatal("preview called discovery")
	}
	presets := SourceImageRules{MinWidth: 1, MinHeight: 1, Ratio: "presets", RatioPresets: []string{"16:9", "1:1"}}
	presetRatio, _ := ParseRatioRule(presets.Ratio, presets.RatioPresets)
	hints := discoveryFilter(presets, presetRatio, []string{"ratio"})
	if hints != nil {
		t.Fatal("preset union narrowed to one provider ratio")
	}
}

func TestHistoryCannotBorrowAnotherMembershipAfterDisableOrRemoval(t *testing.T) {
	body := "version=1\n[[providers]]\nid='wallhaven'\nenabled=true\n[[providers.searches]]\nid='first'\nquery='first'\n[[providers.searches]]\nid='second'\nquery='second'\n"
	service, _, backend := configurationFixture(t, body)
	config, _, err := LoadConfig(service.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	candidate := source.Candidate{ID: "shared", MediaKind: "image", Ref: json.RawMessage(`{"id":"shared"}`), Content: &source.Content{Kind: "https", MediaKind: "image", URL: "https://example.invalid/shared.png"}}
	first, err := service.store.EnsureImage(t.Context(), "wallhaven", "first", config.InputRevision("wallhaven", "first"), "", candidate)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.store.EnsureImage(t.Context(), "wallhaven", "second", config.InputRevision("wallhaven", "second"), "", candidate)
	if err != nil || first.ImageID != second.ImageID {
		t.Fatal("fixture did not share identity")
	}
	path := filepath.Join(t.TempDir(), "image.png")
	writeScanImage(t, path, 2, 2)
	cached, err := service.cache.Materialize(t.Context(), source.Content{Kind: "local", MediaKind: "image", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.store.SaveCachedImage(t.Context(), first.ImageID, catalog.CachedFile{Path: cached.Path, Hash: cached.Hash, Width: cached.Width, Height: cached.Height, Bytes: cached.Bytes}); err != nil {
		t.Fatal(err)
	}
	choice, err := service.materializeChoice(t.Context(), playbackChoice{identity: first, sourceID: "first", historyIndex: -1}, config)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.applyChoice(t.Context(), choice); err != nil {
		t.Fatal(err)
	}
	service.publishStatus()
	status := service.Status()
	saved := settings(t, service)
	disabled, err := service.UpdateSearch(t.Context(), UpdateSearchRequest{Revision: saved.Revision, ID: "first", Enabled: configValue(false)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ReplayHistory(t.Context(), status.Current.ID)
	requireConfigError(t, err, "history_unavailable")
	_, err = service.RemoveSearch(t.Context(), RemoveSearchRequest{Revision: disabled.Revision, ID: "first"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ReplayHistory(t.Context(), status.Current.ID)
	requireConfigError(t, err, "history_unavailable")
	if service.Status().HistoryLength != 1 || service.Status().Current.ID != status.Current.ID || backend.calls.Load() != 1 {
		t.Fatal("unavailable replay retargeted history")
	}
}

func TestSharedRulePreviewOnlyInspectsInheritedSources(t *testing.T) {
	inherited, own := t.TempDir(), t.TempDir()
	writeScanImage(t, filepath.Join(inherited, "wide.png"), 64, 36)
	writeScanImage(t, filepath.Join(own, "square.png"), 64, 64)
	body := localTOML(inherited) + fmt.Sprintf("[[providers.folders]]\nid='own'\npath=%q\n[providers.folders.image_rules]\nmin_width=1\nmin_height=1\nratio='any'\n", own)
	service, _, _ := configurationFixture(t, body)
	refreshForTest(t, service)
	request := draftRequest(t, service)
	request.Rules.Ratio = "16:9"
	preview, err := service.PreviewRules(t.Context(), request)
	if err != nil || preview.Scope != "inherited_catalog" || preview.Inspected != 1 || preview.Matched != 1 || preview.Rejected != 0 {
		t.Fatalf("shared preview counted independently configured source: %+v %v", preview, err)
	}
	before := settings(t, service)
	saved, err := service.UpdateFolder(t.Context(), UpdateFolderRequest{Revision: before.Revision, ID: "home", ImageRules: &SourceImageRules{MinWidth: 1, MinHeight: 1, Ratio: "any"}})
	if err != nil {
		t.Fatal(err)
	}
	request.Revision = saved.Revision
	preview, err = service.PreviewRules(t.Context(), request)
	if err != nil || preview.Inspected != 0 {
		t.Fatalf("shared preview invented affected scope: %+v %v", preview, err)
	}
}

func TestSourcePreviewAfterRulesSaveDoesNotClaimUnscannedRevisionComplete(t *testing.T) {
	root := t.TempDir()
	writeScanImage(t, filepath.Join(root, "wide.png"), 64, 36)
	service, _, _ := configurationFixture(t, localTOML(root))
	refreshForTest(t, service)
	before := settings(t, service)
	saved, err := service.UpdateFolder(t.Context(), UpdateFolderRequest{Revision: before.Revision, ID: "home", ImageRules: &SourceImageRules{MinWidth: 1, MinHeight: 1, Ratio: "16:9"}})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.PreviewRules(t.Context(), RulePreviewRequest{Revision: saved.Revision, ProviderID: "local", InputID: "home", Rules: ImageRules{MinWidth: 1, MinHeight: 1, MaxDepth: saved.Folders[0].EffectiveMaxDepth, Ratio: "16:9"}})
	if err != nil || preview.Complete || preview.Inspected != 0 || preview.Reason != "discovery_incomplete" {
		t.Fatalf("unscanned revision claimed complete scope: %+v %v", preview, err)
	}
}
