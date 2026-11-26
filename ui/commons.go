package main

import (
	"context"
	"time"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/source"
)

type CommonsSearchResult struct {
	Search *source.CommonsSearchPage `json:"commons_search,omitempty"`
	Error  string                    `json:"error,omitempty"`
}

func (c *Client) SearchCommons(query source.CommonsSearch) CommonsSearchResult {
	ctx, cancel := context.WithTimeout(c.ctx, 9*time.Second)
	defer cancel()
	page, err := control.SearchCommons(ctx, c.socket, query)
	if err != nil {
		return CommonsSearchResult{Error: err.Error()}
	}
	return CommonsSearchResult{Search: &page}
}
