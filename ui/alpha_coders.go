package main

import (
	"context"
	"time"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/playback"
	"github.com/vadim82/panora/internal/source"
)

type AlphaResponse struct {
	playback.AlphaResult
	Error string `json:"error,omitempty"`
}

func (c *Client) AlphaCoders(req playback.AlphaRequest) AlphaResponse {
	ctx, cancel := context.WithTimeout(c.ctx, 13*time.Second)
	defer cancel()
	result, err := control.AlphaCoders(ctx, c.socket, req)
	if err != nil {
		return AlphaResponse{Error: err.Error()}
	}
	return AlphaResponse{AlphaResult: result}
}

func (c *Client) OpenAlphaCodersPage(raw string) OpenResult {
	location := source.AlphaCodersPage(raw)
	if location == "" || c.opener == nil {
		return OpenResult{Error: "The wallpaper page is unavailable.", Code: "open_failed"}
	}
	ctx, cancel := context.WithTimeout(c.ctx, 3*time.Second)
	defer cancel()
	if err := c.opener(ctx, location); err != nil {
		return OpenResult{Error: "The wallpaper page could not be opened.", Code: "open_failed"}
	}
	return OpenResult{Opened: true}
}
