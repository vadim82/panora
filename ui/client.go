package main

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/playback"
)

// Client is a thin control-socket consumer. It owns no playback or stored data.
type Client struct {
	ctx             context.Context
	socket          string
	picker          func(context.Context) (string, error)
	savePicker      func(context.Context) (string, error)
	savedFolder     string // guarded by action
	opener          func(context.Context, string) error
	configOpener    func(context.Context, string) error
	action          sync.Mutex
	dialog          sync.Mutex
	manager         serviceManager
	lifecycleAction sync.Mutex
	lifecycleMu     sync.Mutex
	lifecycleBusy   atomic.Bool
	aboutRequested  atomic.Bool
	startupError    string
}

type Snapshot struct {
	Connected    bool                   `json:"connected"`
	Status       *control.Status        `json:"status,omitempty"`
	Error        string                 `json:"error,omitempty"`
	Code         string                 `json:"code,omitempty"`
	Current      *playback.HistoryEntry `json:"current,omitempty"`
	CurrentError string                 `json:"current_error,omitempty"`
}

type Folder struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Canceled bool   `json:"canceled"`
}

func (c *Client) Status() Snapshot {
	if c.socket == "" {
		// The installed Linux window may open only to report a runtime error.
		if message := c.startupMessage(); message != "" {
			return Snapshot{Error: message, Code: "service_socket_unavailable"}
		}
	}
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	status, err := control.Request(ctx, c.socket, "")
	return playbackSnapshot(status, err)
}

func (c *Client) Action(action string) Snapshot {
	return c.playbackAction("A playback action is still running. Refresh status before trying again.", func(ctx context.Context) (control.Status, error) {
		return control.Request(ctx, c.socket, action)
	})
}

func (c *Client) Navigate(input playback.NavigationRequest) Snapshot {
	return c.playbackAction("A playback action is still running. Refresh status before trying again.", func(ctx context.Context) (control.Status, error) {
		return control.Navigate(ctx, c.socket, input)
	})
}

func (c *Client) CancelNavigation(instance, token string) bool {
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	return control.CancelNavigation(ctx, c.socket, instance, token) == nil
}

func (c *Client) playbackAction(busyMessage string, action func(context.Context) (control.Status, error)) Snapshot {
	if !c.action.TryLock() {
		return Snapshot{Error: busyMessage, Code: "busy"}
	}
	defer c.action.Unlock()
	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer cancel()
	status, err := action(ctx)
	if err == nil {
		return playbackSnapshot(status, nil)
	}
	// Reconcile once, preserving the accepted image. Never replay a lost mutation.
	result := c.Status()
	result.Error, result.Code = err.Error(), requestErrorCode(err)
	return result
}

func (c *Client) PickFolder() (Folder, error) {
	return c.pickFolder(c.picker)
}

func (c *Client) PickSaveFolder() (Folder, error) {
	return c.pickFolder(c.savePicker)
}

func (c *Client) pickFolder(picker func(context.Context) (string, error)) (Folder, error) {
	if picker == nil {
		return Folder{}, fmt.Errorf("folder picker unavailable")
	}
	if !c.dialog.TryLock() {
		return Folder{}, fmt.Errorf("a folder picker is already open")
	}
	defer c.dialog.Unlock()
	path, err := picker(c.ctx)
	if err != nil {
		return Folder{}, fmt.Errorf("choose folder: %w", err)
	}
	if path == "" {
		return Folder{Canceled: true}, nil
	}
	if !filepath.IsAbs(path) {
		return Folder{}, fmt.Errorf("folder picker returned a non-absolute path")
	}
	path = filepath.Clean(path)
	return Folder{Name: filepath.Base(path), Path: path}, nil
}

func playbackSnapshot(status control.Status, err error) Snapshot {
	if err != nil {
		return Snapshot{Error: err.Error(), Code: "service_unavailable"}
	}
	result := Snapshot{Connected: true, Status: &status, Current: currentHistoryEntry(status)}
	if status.Current != nil && result.Current == nil {
		result.CurrentError = "Current image details are unavailable. Refresh status to try again."
	}
	return result
}

func currentHistoryEntry(status control.Status) *playback.HistoryEntry {
	entry, image := status.Current, status.CurrentImage
	if entry == nil || image == nil {
		return nil
	}
	return &playback.HistoryEntry{
		ID: entry.ID, ImageID: entry.ImageID, SourceID: entry.SourceID, AcceptedAt: entry.AcceptedAt, Current: true,
		Provider: image.Provider, Image: image.Image, OriginalPath: image.OriginalPath, Dimensions: image.Dimensions, Attribution: image.Attribution,
		Availability: "unverified", Reason: "recheck_on_replay",
	}
}

// SettingsResult keeps save errors distinct from playback status. An uncertain
// save must be reconciled with an explicit read, never an automatic mutation retry.
type SettingsResult struct {
	Configuration *playback.Configuration `json:"configuration,omitempty"`
	Error         string                  `json:"error,omitempty"`
	Code          string                  `json:"code,omitempty"`
}

func (c *Client) Configuration() SettingsResult {
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	config, err := control.ReadConfiguration(ctx, c.socket)
	return settingsResult(config, err)
}

func (c *Client) UpdateConfiguration(edit playback.UpdateConfigurationRequest) SettingsResult {
	return c.editConfiguration(func(ctx context.Context) (playback.Configuration, error) {
		return control.UpdateConfiguration(ctx, c.socket, edit)
	})
}

func settingsResult(config playback.Configuration, err error) SettingsResult {
	if err == nil {
		return SettingsResult{Configuration: &config}
	}
	return SettingsResult{Error: err.Error(), Code: requestErrorCode(err)}
}
