package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/playback"
)

// OpenConfiguration uses the connected service's path, including custom profiles.
// The webview cannot supply a path or URL.
func (c *Client) OpenConfiguration() OpenResult {
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	config, err := control.ReadConfiguration(ctx, c.socket)
	if err != nil {
		return OpenResult{Error: err.Error(), Code: requestErrorCode(err)}
	}
	if c.configOpener == nil || !filepath.IsAbs(config.ConfigPath) {
		return OpenResult{Error: "The configuration file is unavailable for this connection."}
	}
	file, err := os.Stat(config.ConfigPath)
	if err != nil || !file.Mode().IsRegular() {
		return OpenResult{Error: "The configuration path does not point to an available file."}
	}
	if err := c.configOpener(ctx, config.ConfigPath); err != nil {
		return OpenResult{Error: "Panora could not open the configuration file."}
	}
	return OpenResult{Opened: true}
}

func (c *Client) editConfiguration(edit func(context.Context) (playback.Configuration, error)) SettingsResult {
	if !c.action.TryLock() {
		return SettingsResult{Error: "Another operation is still running. Try again when it finishes.", Code: "busy"}
	}
	defer c.action.Unlock()
	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer cancel()
	config, err := edit(ctx)
	return settingsResult(config, err)
}

type CacheResult struct {
	Cache *playback.CacheStatus `json:"cache,omitempty"`
	Error string                `json:"error,omitempty"`
	Code  string                `json:"code,omitempty"`
}

func (c *Client) CacheStatus() CacheResult {
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	result, err := control.ReadCacheStatus(ctx, c.socket)
	if err != nil {
		return CacheResult{Error: err.Error(), Code: requestErrorCode(err)}
	}
	return CacheResult{Cache: &result}
}

type CapabilitiesResult struct {
	Capabilities *playback.Capabilities `json:"capabilities,omitempty"`
	Error        string                 `json:"error,omitempty"`
	Code         string                 `json:"code,omitempty"`
}

func (c *Client) Capabilities() CapabilitiesResult {
	ctx, cancel := context.WithTimeout(c.ctx, 16*time.Second)
	defer cancel()
	result, err := control.ReadCapabilities(ctx, c.socket)
	if err != nil {
		return CapabilitiesResult{Error: err.Error(), Code: requestErrorCode(err)}
	}
	return CapabilitiesResult{Capabilities: &result}
}
