package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const omarchyBarID = "io.github.vadim82.panora"

type omarchyBarSetup struct {
	source        string
	parent        string
	refreshRecord string
	run           func(context.Context, string, ...string) ([]byte, error)
}

func packagedOmarchyBar(socket string) omarchyBarSetup {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return omarchyBarSetup{}
	}
	source := filepath.Join("/usr/share/panora/omarchy", omarchyBarID)
	if _, err := os.Stat(filepath.Join(source, "manifest.json")); err != nil {
		return omarchyBarSetup{}
	}
	if _, err := exec.LookPath("omarchy-shell"); err != nil {
		return omarchyBarSetup{}
	}
	return omarchyBarSetup{
		source:        source,
		parent:        filepath.Join(home, ".config", "omarchy", "plugins"),
		refreshRecord: filepath.Join(filepath.Dir(socket), "omarchy-widget"),
		run:           runOmarchyBarCommand,
	}
}

// A previous registration belongs to the user, including a disabled widget.
// Omarchy's shell config remains the source of truth for enablement and position.
func (b omarchyBarSetup) available() bool {
	if b.parent == "" {
		return false
	}
	for _, id := range []string{omarchyBarID, "vadim82.panora"} {
		if _, err := os.Lstat(filepath.Join(b.parent, id)); !errors.Is(err, os.ErrNotExist) {
			return false
		}
	}
	return true
}

func (b omarchyBarSetup) complete(ctx context.Context) error {
	if b.parent == "" {
		return nil
	}
	registration := filepath.Join(b.parent, omarchyBarID)
	if !b.available() {
		if target, err := os.Readlink(registration); err == nil && target == b.source {
			return b.refresh(ctx)
		}
		return nil
	}
	plugin, err := b.waitForCatalog(ctx, false)
	if err != nil {
		return fmt.Errorf("check existing Omarchy bar registration: %w", err)
	}
	if plugin.ID != "" {
		return nil
	}
	if err := os.MkdirAll(b.parent, 0755); err != nil {
		return fmt.Errorf("create Omarchy plugin directory: %w", err)
	}
	if err := os.Symlink(b.source, registration); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil // Another setup won the race.
		}
		return fmt.Errorf("register packaged Omarchy widget: %w", err)
	}
	// A rejected first attempt can retry on the next start. Keep a link that
	// may already have been enabled if the shell loses the acknowledgement.
	keepRegistration := false
	created, _ := os.Lstat(registration)
	defer func() {
		if !keepRegistration && created != nil {
			if current, statErr := os.Lstat(registration); statErr == nil && os.SameFile(created, current) {
				_ = os.Remove(registration)
			}
		}
	}()
	if _, err := b.run(ctx, "omarchy-shell", "shell", "rescanPlugins"); err != nil {
		return err
	}
	if _, err := b.waitForCatalog(ctx, true); err != nil {
		return fmt.Errorf("wait for Omarchy to discover Panora: %w", err)
	}
	response, err := b.run(ctx, "omarchy-shell", "shell", "enablePlugin", omarchyBarID, "{}")
	keepRegistration = err != nil || strings.TrimSpace(string(response)) == "ok"
	if err != nil {
		return fmt.Errorf("Omarchy bar enablement could not be confirmed: %w", err)
	}
	if !keepRegistration {
		return errors.New("Omarchy declined to enable the Panora bar widget")
	}
	if b.refreshRecord == "" {
		return nil
	}
	entryPoint, err := b.entryPoint()
	if err != nil {
		return err
	}
	return b.confirmRefresh(ctx, entryPoint, true)
}

// Existing links target the same package directory after upgrades. Ask the
// shell to discover the manifest's new component URL without changing layout
// or enablement. Record the refresh for this login session so normal service
// restarts at the same version do not reload the shell's plugins.
func (b omarchyBarSetup) refresh(ctx context.Context) error {
	if b.refreshRecord == "" {
		return nil
	}
	entryPoint, err := b.entryPoint()
	if err != nil {
		return err
	}
	plugin, err := b.waitForCatalog(ctx, false)
	if err != nil {
		return fmt.Errorf("contact Omarchy to refresh the packaged Panora widget: %w", err)
	}
	if plugin.ID == omarchyBarID {
		if plugin.Enabled && b.widgetSourceMatches(ctx, entryPoint) {
			return nil
		}
		if !plugin.Enabled {
			if previous, err := os.ReadFile(b.refreshRecord); err == nil && string(previous) == entryPoint {
				return nil
			}
		}
	}
	if _, err := b.run(ctx, "omarchy-shell", "shell", "rescanPlugins"); err != nil {
		return fmt.Errorf("refresh the packaged Panora widget: %w", err)
	}
	plugin, err = b.waitForCatalog(ctx, true)
	if err != nil {
		return fmt.Errorf("wait for Omarchy to discover the updated widget: %w", err)
	}
	return b.confirmRefresh(ctx, entryPoint, plugin.Enabled)
}

func (b omarchyBarSetup) confirmRefresh(ctx context.Context, entryPoint string, enabled bool) error {
	if enabled {
		if err := b.waitForWidgetSource(ctx, entryPoint); err != nil {
			return err
		}
	}
	if err := os.WriteFile(b.refreshRecord, []byte(entryPoint), 0600); err != nil {
		return fmt.Errorf("record packaged Omarchy widget refresh: %w", err)
	}
	return nil
}

func (b omarchyBarSetup) entryPoint() (string, error) {
	var manifest struct {
		EntryPoints struct {
			BarWidget string
		}
	}
	body, err := os.ReadFile(filepath.Join(b.source, "manifest.json"))
	if err == nil {
		err = json.Unmarshal(body, &manifest)
	}
	if err != nil {
		return "", fmt.Errorf("read packaged Omarchy widget manifest: %w", err)
	}
	if manifest.EntryPoints.BarWidget == "" {
		return "", errors.New("packaged Omarchy widget manifest has no bar entry point")
	}
	return manifest.EntryPoints.BarWidget, nil
}

func (b omarchyBarSetup) widgetSourceMatches(ctx context.Context, entryPoint string) bool {
	source, err := b.run(ctx, "omarchy-shell", omarchyBarID, "widgetSource")
	return err == nil && strings.HasSuffix(strings.TrimSpace(string(source)), "/"+entryPoint)
}

func (b omarchyBarSetup) waitForWidgetSource(ctx context.Context, entryPoint string) error {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		if b.widgetSourceMatches(ctx, entryPoint) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("could not confirm Omarchy loaded the new Panora widget (%s): %w", entryPoint, ctx.Err())
		case <-tick.C:
		}
	}
}

type omarchyBarPlugin struct {
	ID      string
	Enabled bool
}

// A starting shell can answer the background readiness probe before serving
// its plugin catalog. Rescans also return before local discovery is complete.
func (b omarchyBarSetup) waitForCatalog(ctx context.Context, requirePanora bool) (omarchyBarPlugin, error) {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		plugin, err := b.discovered(ctx)
		if err == nil && (plugin.ID == omarchyBarID || !requirePanora) {
			return plugin, nil
		}
		select {
		case <-ctx.Done():
			if err != nil {
				return omarchyBarPlugin{}, err
			}
			return omarchyBarPlugin{}, ctx.Err()
		case <-tick.C:
		}
	}
}

func (b omarchyBarSetup) discovered(ctx context.Context) (omarchyBarPlugin, error) {
	output, err := b.run(ctx, "omarchy-shell", "shell", "listPlugins")
	if err != nil {
		return omarchyBarPlugin{}, err
	}
	var plugins []omarchyBarPlugin
	if err := json.Unmarshal(output, &plugins); err != nil {
		return omarchyBarPlugin{}, fmt.Errorf("read Omarchy plugin list: %w", err)
	}
	var legacy omarchyBarPlugin
	for _, plugin := range plugins {
		if plugin.ID == omarchyBarID {
			return plugin, nil
		}
		if plugin.ID == "vadim82.panora" {
			legacy = plugin
		}
	}
	return legacy, nil
}

type omarchyBarOutput struct {
	buffer bytes.Buffer
	cancel context.CancelFunc
}

func (out *omarchyBarOutput) Write(p []byte) (int, error) {
	if len(p) > (256<<10)-out.buffer.Len() {
		out.cancel()
		return 0, errors.New("Omarchy plugin response exceeds 256 KiB")
	}
	return out.buffer.Write(p)
}

func runOmarchyBarCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	commandCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, name, args...)
	out := omarchyBarOutput{cancel: cancel}
	cmd.Stdout = &out
	cmd.WaitDelay = 250 * time.Millisecond
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("Omarchy %s: %w", args[len(args)-1], err)
	}
	return out.buffer.Bytes(), nil
}
