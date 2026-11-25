package main

import (
	"context"
	"net/http"
	"time"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/playback"
)

type OmarchyThemeResult struct {
	Theme *playback.OmarchyThemePreview `json:"theme,omitempty"`
	Error string                        `json:"error,omitempty"`
}

func (c *Client) OmarchyThemePreview(page, limit int) OmarchyThemeResult {
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	result, err := control.ReadOmarchyTheme(ctx, c.socket, page, limit)
	if err != nil {
		return OmarchyThemeResult{Error: err.Error()}
	}
	return OmarchyThemeResult{Theme: &result}
}

func (c *Client) EditOmarchyTheme(action string, edit playback.EditOmarchyThemeRequest) SettingsResult {
	method := map[string]string{"add": http.MethodPost, "update": http.MethodPatch, "remove": http.MethodDelete}[action]
	if method == "" {
		return SettingsResult{Code: "invalid_request", Error: "Unknown Omarchy theme source operation."}
	}
	return c.editConfiguration(func(ctx context.Context) (playback.Configuration, error) {
		return control.EditOmarchyTheme(ctx, c.socket, method, edit)
	})
}
