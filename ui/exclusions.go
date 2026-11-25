package main

import (
	"context"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/playback"
)

func (c *Client) ExcludeCurrent(instance, historyID string, imageID int64) Snapshot {
	return c.playbackAction("A playback action is still running. Refresh status before trying again.", func(ctx context.Context) (control.Status, error) {
		return control.ExcludeCurrent(ctx, c.socket, instance, playback.HistoryReference{ID: historyID, ImageID: imageID})
	})
}
