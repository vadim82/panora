package cache

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/source"
)

func TestHTTPFailuresKeepCategoryAndRetryHints(t *testing.T) {
	remote := source.Content{MediaKind: "image", Kind: "https", URL: "https://images.example.com/original.png"}
	for _, tt := range []struct {
		status int
		code   string
	}{{503, "unavailable"}, {429, "rate_limited"}, {401, "auth_required"}, {403, "auth_rejected"}, {404, "not_found"}} {
		c := newCache(t, 0, 0)
		c.client = &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: tt.status, Header: http.Header{"Retry-After": []string{"30"}}, Body: io.NopCloser(strings.NewReader("SECRET response"))}, nil
		})}
		_, err := c.Materialize(t.Context(), remote)
		var typed *source.Error
		var staged *Error
		if !errors.As(err, &typed) || typed.Code != tt.code || typed.RetryAfterSeconds == nil || *typed.RetryAfterSeconds != 30 || !errors.As(err, &staged) || staged.Stage != "download" {
			t.Fatalf("typed HTTP failure lost: %v", err)
		}
		if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "https") {
			t.Fatal("raw download diagnostic escaped")
		}
	}
	c := newCache(t, 0, 0)
	c.client = &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("bad image bytes"))}, nil
	})}
	_, err := c.Materialize(t.Context(), remote)
	var staged *Error
	if !errors.As(err, &staged) || staged.Stage != "decode" || staged.Code != "invalid_content" {
		t.Fatalf("bad bytes conflated with network failure: %v", err)
	}
	date := time.Now().Add(10 * time.Second).UTC().Format(http.TimeFormat)
	if hint := source.RetryAfter(date, time.Now()); hint == nil || *hint < 9 || *hint > 10 {
		t.Fatal("date hint lost")
	}
}
