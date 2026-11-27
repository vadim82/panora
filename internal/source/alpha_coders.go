package source

import (
	"context"
	"encoding/json"
)

type AlphaTag struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Total int    `json:"wallpaper_count,omitempty"`
}

type AlphaSample struct {
	Title     string `json:"title"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Thumbnail string `json:"thumbnail"`
	Page      string `json:"page,omitempty"`
}

type AlphaScope struct {
	Name  string `json:"name"`
	Pages int    `json:"pages"`
	Count int    `json:"count"`
	More  bool   `json:"more"`
	Error string `json:"error,omitempty"`
}

type AlphaCheck struct {
	Count   int           `json:"count"`
	Scopes  []AlphaScope  `json:"scopes"`
	Samples []AlphaSample `json:"samples"`
}

type AlphaEditor interface {
	Connected() bool
	SetKey(context.Context, string, bool) error
	Tags(context.Context, string) ([]AlphaTag, error)
	Check(context.Context, json.RawMessage, *FilterHints, func(Dimensions) bool) (AlphaCheck, error)
	Thumbnail(context.Context, string) (string, error)
}
