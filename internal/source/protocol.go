// Package source defines the versioned, language-independent source protocol.
// It knows nothing about desktop rendering, playback state, or image eligibility.
package source

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	Protocol         = 2
	JSONRPC          = "2.0"
	MaxBatchSize     = 100
	MaxPageSize      = 100
	MaxRequestBytes  = 64 << 10
	MaxResponseBytes = 1 << 20
	MaxCursorBytes   = 4 << 10
)

func IsNull(data json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(data), []byte("null")) }

type Error struct {
	Code              string `json:"code"`
	Message           string `json:"message"`
	RetryAfterSeconds *int64 `json:"retry_after_seconds,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func Failure(code, message string) *Error { return &Error{Code: code, Message: message} }

// Request and Response are JSON-RPC 2.0 envelopes. IDs stay raw so a provider
// can echo string, numeric, or null IDs without losing numeric precision.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error deliberately excludes data: arbitrary remote diagnostics may be secret.
func (e *RPCError) Error() string { return fmt.Sprintf("provider RPC error %d", e.Code) }

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type Field struct {
	Key   string `json:"key"`
	Type  string `json:"type"`
	Label string `json:"label"`
}

type Description struct {
	Protocol     int      `json:"protocol"`
	Requirements []string `json:"requires,omitempty"`
	FilterHints  []string `json:"filter_hints,omitempty"`
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	MediaKinds   []string `json:"media_kinds"`
	ContentKinds []string `json:"content_kinds"`
	Operations   []string `json:"operations"`
	Fields       []Field  `json:"fields"`
}

// LocalConfig is local discovery configuration, not common image filters.
type LocalConfig struct {
	Revision string `json:"revision,omitempty"`
	Root     string `json:"root"`
	MaxDepth int    `json:"max_depth"`
}

type DiscoverParams struct {
	Config json.RawMessage `json:"config"`
	Cursor json.RawMessage `json:"cursor"`
	Limit  int             `json:"limit"`
	Filter *FilterHints    `json:"filter,omitempty"`
}

type Content struct {
	MediaKind string `json:"media_kind"`
	Kind      string `json:"kind"`
	Path      string `json:"path,omitempty"`
	URL       string `json:"url,omitempty"`
	MIMEType  string `json:"mime_type,omitempty"`
}

// Content is an optional already-known location. Ref remains opaque to the host.
// A local listing supplies Content so eligibility needs no subprocess per file.
type Candidate struct {
	Dimensions   *Dimensions     `json:"dimensions,omitempty"`
	Attribution  *Attribution    `json:"attribution,omitempty"`
	Requirements []string        `json:"requires,omitempty"`
	ID           string          `json:"id"`
	MediaKind    string          `json:"media_kind"`
	Ref          json.RawMessage `json:"ref"`
	Content      *Content        `json:"content,omitempty"`
}

type Page struct {
	Candidates []Candidate     `json:"candidates"`
	NextCursor json.RawMessage `json:"next_cursor"`
}

type ResolveParams struct {
	Config json.RawMessage `json:"config"`
	ID     string          `json:"id"`
	Ref    json.RawMessage `json:"ref"`
}

type Resolved struct {
	Content      Content      `json:"content"`
	Dimensions   *Dimensions  `json:"dimensions,omitempty"`
	Attribution  *Attribution `json:"attribution,omitempty"`
	Requirements []string     `json:"requires,omitempty"`
}

type Provider interface {
	Describe(context.Context) (Description, error)
	Validate(context.Context, json.RawMessage) error
	Discover(context.Context, DiscoverParams) (Page, error)
	Resolve(context.Context, ResolveParams) (Resolved, error)
}

func Decode(data []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return fmt.Errorf("expected one JSON value")
	}
	return nil
}
