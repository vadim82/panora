package alphacoders

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/vadim82/panora/internal/source"
)

func TestCredentialValidationAndRedaction(t *testing.T) {
	p := New(filepath.Join(t.TempDir(), "profile.key"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("auth")
		if r.URL.Path == "/query-count" && key == "test-credential" {
			_ = json.NewEncoder(w).Encode(response{Success: true})
		} else {
			_, _ = w.Write([]byte(`{"success":false,"error":"invalid_auth","detail":"not-for-user"}`))
		}
	}))
	defer server.Close()
	p.base = server.URL
	if p.Connected() {
		t.Fatal("unexpected stored credential")
	}
	if err := p.SetKey(t.Context(), "test-credential", false); err != nil || !p.Connected() {
		t.Fatalf("connect: %v", err)
	}
	info, err := os.Stat(p.credential)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("expected owner-only credential: %v", err)
	}
	err = p.SetKey(t.Context(), "invalid-secret", false)
	if err == nil || strings.Contains(err.Error(), "invalid-secret") || strings.Contains(err.Error(), "not-for-user") {
		t.Fatalf("expected a sanitized authentication error, got %v", err)
	}
	if key, err := p.key(); err != nil || key != "test-credential" {
		t.Fatal("replacement modified the working key")
	}
	if err := p.SetKey(t.Context(), "", true); err != nil || p.Connected() {
		t.Fatalf("remove: %v", err)
	}
}

func TestTagDiscoveryCheckAndOriginal(t *testing.T) {
	first := item{ID: 91, Title: "Nebula", Type: "webp", Checksum: "same", Width: 5120, Height: 1440,
		Original: "https://images2.alphacoders.com/1/91.webp",
		Thumb:    "https://images2.alphacoders.com/1/thumb-350-91.webp",
		Page:     "https://wall.alphacoders.com/big.php?i=81"}
	second := first
	second.ID, second.Checksum = 92, "second"
	unsupported := first
	unsupported.ID, unsupported.Type = 93, "mp4"
	p := New(filepath.Join(t.TempDir(), "key"))
	if err := writeKey(p.credential, "test-credential"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("auth") != "test-credential" {
			t.Error("upstream did not receive the key")
		}
		result := response{Success: true}
		switch r.URL.Path {
		case "/tags":
			if q.Get("term") != "neb" {
				t.Error("missing prefix")
			}
			result.Tags = []Tag{{ID: 1, Name: "Nebula", Total: 12}}
		case "/tags/1", "/tags/2":
			if q.Get("width") != "3840" || q.Get("height") != "1080" || q.Get("operator") != "min" ||
				q.Get("min_ratio") != "3.200000" || q.Get("max_ratio") != "3.911112" {
				t.Error("missing upstream size and ratio filters")
			}
			result.Contents = []item{first, unsupported}
			if r.URL.Path == "/tags/2" {
				result.Contents = []item{first, second}
			}
		case "/contents/91":
			result.Content = first
		default:
			t.Error("unexpected endpoint", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(result)
	}))
	defer server.Close()
	p.base = server.URL
	tags, err := p.Tags(t.Context(), "neb")
	if err != nil || len(tags) != 1 || tags[0].Total != 12 {
		t.Fatalf("tag results: %+v, %v", tags, err)
	}
	cfg := json.RawMessage(`{"tags":[{"id":1,"name":"Nebula"},{"id":2,"name":"Space"}]}`)
	filter := &source.FilterHints{MinWidth: 3840, MinHeight: 1080, RatioRange: &source.RatioBounds{Min: 3.2, Max: 3.9111111}}
	page, err := p.Discover(t.Context(), source.DiscoverParams{Config: cfg, Limit: 30, Filter: filter})
	if err != nil || len(page.Candidates) != 1 || source.IsNull(page.NextCursor) {
		t.Fatalf("discovery: %+v, %v", page, err)
	}
	next, err := p.Discover(t.Context(), source.DiscoverParams{Config: cfg, Cursor: page.NextCursor, Limit: 30, Filter: filter})
	if err != nil || len(next.Candidates) != 2 || !source.IsNull(next.NextCursor) {
		t.Fatalf("next tag: %+v, %v", next, err)
	}
	check, err := p.Check(t.Context(), cfg, filter, func(d source.Dimensions) bool { return d.Width >= 5120 })
	if err != nil || check.Count != 2 || len(check.Samples) != 2 || check.Scopes[0].Count != 1 || check.Scopes[1].Count != 2 {
		t.Fatalf("distinct result counts: %+v, %v", check, err)
	}
	candidate := page.Candidates[0]
	resolved, err := p.Resolve(t.Context(), source.ResolveParams{ID: candidate.ID, Ref: candidate.Ref})
	if err != nil || resolved.Content.URL != first.Original || resolved.Attribution.PageURL != first.Page || resolved.Validate() != nil {
		t.Fatalf("resolve: %+v, %v", resolved, err)
	}
}

func TestRateLimitIsSharedAcrossOperations(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "100")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	p := New(filepath.Join(t.TempDir(), "key"))
	p.base = server.URL
	if err := writeKey(p.credential, "test-credential"); err != nil {
		t.Fatal(err)
	}
	_, first := p.Tags(t.Context(), "space")
	_, second := p.Discover(t.Context(), source.DiscoverParams{Config: json.RawMessage("{}"), Limit: 30})
	for _, err := range []error{first, second} {
		var failure *source.Error
		if !errors.As(err, &failure) || failure.Code != "rate_limited" || failure.RetryAfterSeconds == nil {
			t.Fatalf("expected shared retry: %v", err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("sent %d requests", calls.Load())
	}
}
