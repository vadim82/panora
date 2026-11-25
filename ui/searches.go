package main

import (
	"context"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/playback"
)

func (c *Client) AddSearch(edit playback.AddSearchRequest) SettingsResult {
	return c.editConfiguration(func(ctx context.Context) (playback.Configuration, error) {
		return control.AddSearch(ctx, c.socket, edit)
	})
}

func (c *Client) UpdateSearch(edit playback.UpdateSearchRequest) SettingsResult {
	return c.editConfiguration(func(ctx context.Context) (playback.Configuration, error) {
		return control.UpdateSearch(ctx, c.socket, edit)
	})
}

func (c *Client) RemoveSearch(edit playback.RemoveSearchRequest) SettingsResult {
	return c.editConfiguration(func(ctx context.Context) (playback.Configuration, error) {
		return control.RemoveSearch(ctx, c.socket, edit)
	})
}
