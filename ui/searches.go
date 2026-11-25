package main

import (
	"context"
	"time"

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

type SearchCandidatesResult struct {
	Counts *playback.SearchCandidates `json:"counts,omitempty"`
	Error  string                     `json:"error,omitempty"`
}

func (c *Client) CountSearchCandidates(id, revision string) SearchCandidatesResult {
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	result, err := control.CountSearchCandidates(ctx, c.socket, id, revision)
	if err != nil {
		return SearchCandidatesResult{Error: err.Error()}
	}
	return SearchCandidatesResult{Counts: &result}
}
