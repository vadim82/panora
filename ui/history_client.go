package main

import (
	"context"
	"fmt"
	"time"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/playback"
)

type HistoryResult struct {
	Page  *playback.HistoryPage `json:"page,omitempty"`
	Error string                `json:"error,omitempty"`
	Code  string                `json:"code,omitempty"`
}

type ExclusionsResult struct {
	Page  *playback.ExclusionPage `json:"page,omitempty"`
	Error string                  `json:"error,omitempty"`
	Code  string                  `json:"code,omitempty"`
}

func (c *Client) History(before string) HistoryResult {
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	page, err := control.ListHistory(ctx, c.socket, before, playback.MaxHistoryPageSize)
	if err != nil {
		return HistoryResult{Error: err.Error(), Code: requestErrorCode(err)}
	}
	return HistoryResult{Page: &page}
}

func (c *Client) Exclusions(before int64) ExclusionsResult {
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	page, err := control.ListExclusions(ctx, c.socket, before, playback.MaxExclusionsPageSize)
	if err != nil {
		return ExclusionsResult{Error: err.Error(), Code: requestErrorCode(err)}
	}
	return ExclusionsResult{Page: &page}
}

func (c *Client) ReplayHistory(id string) Snapshot {
	return c.playbackAction("Another playback action is running. Try again when it finishes.", func(ctx context.Context) (control.Status, error) {
		return control.ReplayHistory(ctx, c.socket, id)
	})
}

func (c *Client) RestoreExclusion(id int64) Snapshot {
	return c.playbackAction("Another playback action is running. Try again when it finishes.", func(ctx context.Context) (control.Status, error) {
		return control.RestoreExclusion(ctx, c.socket, id)
	})
}

// OpenHistory accepts only a retained handle and image identity. Every location
// comes from a fresh bounded service read, never a location supplied by the UI.
func (c *Client) OpenHistory(id string, imageID int64, target string) OpenResult {
	if id == "" || len(id) > 128 || imageID < 1 || target != "page" && target != "folder" {
		return OpenResult{Error: "Choose a retained history image and its original location.", Code: "invalid_request"}
	}
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	entry, err := c.findHistoryEntry(ctx, id, imageID)
	if err != nil {
		return OpenResult{Error: "This history image could not be checked right now. Reload history to check its availability.", Code: "target_unavailable"}
	}
	location, err := openLocation(*entry, target)
	if err != nil {
		return OpenResult{Error: err.Error(), Code: "target_unavailable"}
	}
	if c.opener == nil {
		return OpenResult{Error: "Opening locations is unavailable in this client.", Code: "unsupported"}
	}
	if err := c.opener(ctx, location); err != nil {
		return OpenResult{Error: "The original location could not be opened.", Code: "open_failed"}
	}
	return OpenResult{Opened: true}
}

// An older accepted image can lie beyond page one. Bound reads by the
// retention limit even if the peer cycles through malformed cursors.
func (c *Client) findHistoryEntry(ctx context.Context, id string, imageID int64) (*playback.HistoryEntry, error) {
	before := ""
	for offset := 0; offset < playback.HistoryLimit; offset += playback.MaxHistoryPageSize {
		page, err := control.ListHistory(ctx, c.socket, before, playback.MaxHistoryPageSize)
		if err != nil {
			return nil, err
		}
		for _, entry := range page.Entries {
			if entry.ID == id && entry.ImageID == imageID {
				return &entry, nil
			}
		}
		if page.NextBefore == "" || page.NextBefore == before {
			break
		}
		before = page.NextBefore
	}
	return nil, fmt.Errorf("history entry is not retained")
}
