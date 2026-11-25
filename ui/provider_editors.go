package main

import (
	"context"
	"net/http"
	"time"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/playback"
)

func (c *Client) AddProviderInput(edit playback.ProviderInputEdit) SettingsResult {
	return c.editProviderInput(http.MethodPost, edit)
}

func (c *Client) UpdateProviderInput(edit playback.ProviderInputEdit) SettingsResult {
	return c.editProviderInput(http.MethodPatch, edit)
}

func (c *Client) RemoveProviderInput(edit playback.ProviderInputEdit) SettingsResult {
	return c.editProviderInput(http.MethodDelete, edit)
}

func (c *Client) editProviderInput(method string, edit playback.ProviderInputEdit) SettingsResult {
	return c.editConfiguration(func(ctx context.Context) (playback.Configuration, error) {
		return control.EditProviderInput(ctx, c.socket, method, edit)
	})
}

type ProviderSuggestions struct {
	Values []string `json:"values,omitempty"`
	Error  string   `json:"error,omitempty"`
}

func (c *Client) SuggestProviderInput(query control.ProviderSuggestRequest) ProviderSuggestions {
	ctx, cancel := context.WithTimeout(c.ctx, 2*time.Second)
	defer cancel()
	result, err := control.SuggestProviderInput(ctx, c.socket, query)
	if err != nil {
		return ProviderSuggestions{Error: "Provider suggestions are unavailable."}
	}
	return ProviderSuggestions{Values: result.Values}
}
