package main

import (
	"context"
	"time"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/playback"
)

type RulePreviewResult struct {
	Preview *playback.RulePreview `json:"preview,omitempty"`
	Error   string                `json:"error,omitempty"`
	Code    string                `json:"code,omitempty"`
}

// RulePreview inspects an unsaved service-owned rule. It never applies the
// draft, chooses a wallpaper, or retries the read as a mutation.
func (c *Client) RulePreview(request playback.RulePreviewRequest) RulePreviewResult {
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	result, err := control.PreviewRules(ctx, c.socket, request)
	if err != nil {
		return RulePreviewResult{Error: err.Error(), Code: requestErrorCode(err)}
	}
	return RulePreviewResult{Preview: &result}
}
