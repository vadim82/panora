package main

import (
	"context"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/playback"
)

func (c *Client) AddFolderSettings(edit playback.AddFolderRequest) SettingsResult {
	return c.editConfiguration(func(ctx context.Context) (playback.Configuration, error) {
		return control.AddFolder(ctx, c.socket, edit)
	})
}
func (c *Client) UpdateFolder(edit playback.UpdateFolderRequest) SettingsResult {
	return c.editConfiguration(func(ctx context.Context) (playback.Configuration, error) {
		return control.UpdateFolder(ctx, c.socket, edit)
	})
}
func (c *Client) RemoveFolder(edit playback.RemoveFolderRequest) SettingsResult {
	return c.editConfiguration(func(ctx context.Context) (playback.Configuration, error) {
		return control.RemoveFolder(ctx, c.socket, edit)
	})
}
