package wallhaven

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/source"
)

func fixtureProvider(t *testing.T, handler http.HandlerFunc) *Provider {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Provider{client: client, baseURL: server.URL}
}
func item(id string) map[string]any {
	return map[string]any{"id": id, "url": "https://wallhaven.cc/w/" + id, "path": "https://w.wallhaven.cc/full/aa/wallhaven-" + id + ".png", "purity": "sfw", "file_type": "image/png", "dimension_x": 5120, "dimension_y": 1440, "source": "https://artist.example.org/work", "uploader": map[string]any{"username": "Uploader", "url": "https://wallhaven.cc/user/Uploader"}}
}
func listingReply(w http.ResponseWriter, page, last int, seed string, items []map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": items, "meta": map[string]any{"current_page": page, "last_page": last, "per_page": 24, "seed": seed}})
}
func requireKind(t *testing.T, err error, kind string) *source.Error {
	t.Helper()
	var typed *source.Error
	if !errors.As(err, &typed) || typed.Code != kind {
		t.Fatalf("want %s, got %v", kind, err)
	}
	return typed
}

func TestSettingsAndDescription(t *testing.T) {
	p := New()
	d, err := p.Describe(context.Background())
	if err != nil || d.ID != "wallhaven" || d.Protocol != source.Protocol || len(d.FilterHints) != 5 {
		t.Fatalf("description: %+v %v", d, err)
	}
	c, err := parseConfig(context.Background(), json.RawMessage(`{"query":"nature"}`))
	if err != nil || c.Categories != "100" || c.Sorting != "relevance" || c.MaxPages != 20 {
		t.Fatalf("defaults: %+v %v", c, err)
	}
	for _, raw := range []string{`null`, `{"query":"nature","apikey":"secret"}`, `{"query":"nature","categories":"12"}`, `{"query":"nature","sorting":"other"}`, `{"query":"nature","order":"other"}`, `{"query":"nature","max_pages":101}`, `{"query":"nature","max_pages":0}`, `{"query":"nature","top_range":"other"}`, `{"query":"nature","purity":"111"}`} {
		requireKind(t, p.Validate(context.Background(), json.RawMessage(raw)), "invalid_config")
	}
}

func TestPagingFilterMappingsAndResolve(t *testing.T) {
	calls := 0
	p := fixtureProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/w/aa0002" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": item("aa0002")})
			return
		}
		if r.URL.Path != "/search" {
			t.Errorf("unexpected route %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		for key, want := range map[string]string{"q": "nature", "categories": "100", "purity": "100", "sorting": "relevance", "order": "desc", "atleast": "5120x1440", "ratios": "32x9"} {
			if q.Get(key) != want {
				t.Errorf("%s=%s want %s", key, q.Get(key), want)
			}
		}
		if q.Get("page") == "1" {
			listingReply(w, 1, 2, "", []map[string]any{item("aa0001"), item("aa0002")})
		} else {
			listingReply(w, 2, 2, "", []map[string]any{item("aa0003")})
		}
	})
	params := source.DiscoverParams{Config: json.RawMessage(`{"query":"nature"}`), Limit: 1, Filter: &source.FilterHints{MinWidth: 5120, MinHeight: 1440, Ratio: &source.Dimensions{Width: 32, Height: 9}}}
	var ids []string
	var chosen source.Candidate
	for i := 0; i < 3; i++ {
		page, err := p.Discover(context.Background(), params)
		if err != nil || len(page.Candidates) != 1 {
			t.Fatalf("page: %+v %v", page, err)
		}
		c := page.Candidates[0]
		ids = append(ids, c.ID)
		chosen = c
		params.Cursor = page.NextCursor
		if i < 2 && source.IsNull(page.NextCursor) {
			t.Fatal("lost next cursor")
		}
	}
	if strings.Join(ids, ",") != "aa0001,aa0002,aa0003" || !source.IsNull(params.Cursor) {
		t.Fatalf("pagination lost data: %v %s", ids, params.Cursor)
	}
	resolved, err := p.Resolve(context.Background(), source.ResolveParams{Config: params.Config, ID: "aa0002", Ref: json.RawMessage(`{"id":"aa0002"}`)})
	if err != nil || resolved.Dimensions.Width != 5120 || resolved.Attribution.Author != "Uploader" || resolved.Attribution.SourceURL != "https://artist.example.org/work" || resolved.Content.URL == "" {
		t.Fatalf("resolve metadata: %+v %v", resolved, err)
	}
	if calls != 4 || chosen.Content.MIMEType != "image/png" {
		t.Fatalf("unexpected requests/content: %d %+v", calls, chosen)
	}
}

func TestRandomSeedAndCursorSettingsBinding(t *testing.T) {
	p := fixtureProvider(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("page") == "2" && q.Get("seed") != "abc123" {
			t.Errorf("seed lost: %s", q.Get("seed"))
		}
		page := 1
		if q.Get("page") == "2" {
			page = 2
		}
		listingReply(w, page, 2, "abc123", []map[string]any{item(fmt.Sprintf("aa000%d", page))})
	})
	params := source.DiscoverParams{Config: json.RawMessage(`{"query":"nature","sorting":"random"}`), Limit: 24}
	first, err := p.Discover(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	params.Cursor = first.NextCursor
	second, err := p.Discover(context.Background(), params)
	if err != nil || second.Candidates[0].ID != "aa0002" {
		t.Fatalf("random page: %+v %v", second, err)
	}
	changed := params
	changed.Config = json.RawMessage(`{"query":"space","sorting":"random"}`)
	_, err = p.Discover(context.Background(), changed)
	requireKind(t, err, "invalid_cursor")
	changed = params
	changed.Filter = &source.FilterHints{MinWidth: 2}
	_, err = p.Discover(context.Background(), changed)
	requireKind(t, err, "invalid_cursor")
	changed = params
	changed.Cursor = json.RawMessage(`{"config":"old","page":1,"offset":0}`)
	_, err = p.Discover(context.Background(), changed)
	requireKind(t, err, "invalid_cursor")
}

func TestChangedRatioCannotSendAnOldPageRequest(t *testing.T) {
	requests := make(chan string, 3)
	p := fixtureProvider(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		requests <- q.Get("ratios") + "/" + q.Get("atleast") + "/" + q.Get("page")
		listingReply(w, 1, 2, "", []map[string]any{item("aa0001")})
	})
	params := source.DiscoverParams{
		Config: json.RawMessage(`{"query":"nature"}`), Limit: 24,
		Filter: &source.FilterHints{MinWidth: 5120, MinHeight: 1440, Ratio: &source.Dimensions{Width: 32, Height: 9}},
	}
	first, err := p.Discover(t.Context(), params)
	if err != nil || source.IsNull(first.NextCursor) {
		t.Fatalf("first page did not return a cursor: %s %v", first.NextCursor, err)
	}
	params.Cursor = first.NextCursor
	params.Filter.Ratio = &source.Dimensions{Width: 16, Height: 9}
	_, err = p.Discover(t.Context(), params)
	requireKind(t, err, "invalid_cursor")
	params.Cursor = json.RawMessage("null")
	if _, err = p.Discover(t.Context(), params); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || <-requests != "32x9/5120x1440/1" || <-requests != "16x9/5120x1440/1" {
		t.Fatal("a changed ratio reused an old page or dropped the minimum dimensions")
	}
}

func TestEmptyUnsupportedAndPageCap(t *testing.T) {
	for _, mode := range []string{"empty", "unsupported", "cap"} {
		t.Run(mode, func(t *testing.T) {
			p := fixtureProvider(t, func(w http.ResponseWriter, r *http.Request) {
				items := []map[string]any{}
				if mode == "unsupported" {
					a, b := item("aa0001"), item("aa0002")
					a["purity"] = "nsfw"
					b["file_type"] = "image/gif"
					items = append(items, a, b)
				}
				if mode == "cap" {
					items = append(items, item("aa0001"))
				}
				listingReply(w, 1, 3, "", items)
			})
			page, err := p.Discover(context.Background(), source.DiscoverParams{Config: json.RawMessage(`{"query":"nature","max_pages":1}`), Limit: 24})
			if err != nil || !source.IsNull(page.NextCursor) {
				t.Fatalf("page cap/empty: %+v %v", page, err)
			}
			if mode != "cap" && len(page.Candidates) != 0 {
				t.Fatal("unsupported result became playable")
			}
		})
	}
}

func TestHTTPFailuresPrivacyAndRetryHints(t *testing.T) {
	for _, tc := range []struct {
		status int
		kind   string
	}{{401, "auth_required"}, {403, "auth_rejected"}, {404, "not_found"}, {429, "rate_limited"}, {503, "unavailable"}} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			p := fixtureProvider(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "120")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte("private secret response"))
			})
			_, err := p.Discover(context.Background(), source.DiscoverParams{Config: json.RawMessage(`{"query":"nature"}`), Limit: 24})
			typed := requireKind(t, err, tc.kind)
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("remote body leaked")
			}
			if tc.status == 429 || tc.status == 503 {
				if typed.RetryAfterSeconds == nil || *typed.RetryAfterSeconds != 120 {
					t.Fatal("retry hint lost")
				}
			}
		})
	}
	if retryHint("bad") != nil || *retryHint("999999") != 86400 || *retryHint(time.Now().Add(2 * time.Minute).UTC().Format(http.TimeFormat)) < 100 {
		t.Fatal("retry hint parsing failed")
	}
}

func TestHTTPResponseBoundsMalformedAndRedirect(t *testing.T) {
	for _, mode := range []string{"oversized", "malformed", "html", "redirect", "wrong metadata"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			p := fixtureProvider(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				switch mode {
				case "oversized":
					_, _ = w.Write([]byte(strings.Repeat("x", maxBodyBytes+1)))
				case "malformed":
					_, _ = w.Write([]byte(`{"data":`))
				case "html":
					w.Header().Set("Content-Type", "text/html")
				case "redirect":
					http.Redirect(w, r, "/other", http.StatusFound)
				case "wrong metadata":
					listingReply(w, 2, 2, "", []map[string]any{})
				}
			})
			_, err := p.Discover(context.Background(), source.DiscoverParams{Config: json.RawMessage(`{"query":"nature"}`), Limit: 24})
			requireKind(t, err, "unavailable")
			if calls != 1 {
				t.Fatal("redirect followed")
			}
		})
	}
}

func TestCancellationAndInvalidResolveReference(t *testing.T) {
	started := make(chan struct{})
	p := fixtureProvider(t, func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := p.Discover(ctx, source.DiscoverParams{Config: json.RawMessage(`{"query":"nature"}`), Limit: 24})
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("request did not cancel")
	}
	_, err := p.Resolve(context.Background(), source.ResolveParams{Config: json.RawMessage(`{"query":"nature"}`), ID: "aa0001", Ref: json.RawMessage(`{"id":"aa0002"}`)})
	requireKind(t, err, "invalid_config")
}

func TestMetadataValidationAndUnknownDimensions(t *testing.T) {
	w := wallpaper{ID: "aa0001", URL: "https://wallhaven.cc/w/aa0001", Path: "https://w.wallhaven.cc/full/aa/a.png", MIME: "image/png"}
	c, err := candidate(w)
	if err != nil || c.Dimensions != nil {
		t.Fatalf("unknown dimensions: %+v %v", c, err)
	}
	for _, mutate := range []func(*wallpaper){func(w *wallpaper) { w.Path = "https://user:secret@w.wallhaven.cc/a.png" }, func(w *wallpaper) { w.Path = "https://w.wallhaven.cc/a.png?token=secret" }, func(w *wallpaper) { w.Width = 1 }, func(w *wallpaper) { w.URL = "https://other.example.org" }, func(w *wallpaper) { w.ID = "../bad" }} {
		changed := w
		mutate(&changed)
		_, err := candidate(changed)
		requireKind(t, err, "unavailable")
	}
	w.Source = "https://artist.example.org/?token=secret"
	c, err = candidate(w)
	if err != nil || c.Attribution.SourceURL != "" {
		t.Fatal("unsafe source link retained")
	}
}

func TestFilterOnlyAndGeometryMappings(t *testing.T) {
	p := fixtureProvider(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		want := map[string]string{"categories": "111", "purity": "100"}
		if q.Get("resolutions") != "" {
			want["q"] = `+mountains +"night sky" city -cars`
			want["resolutions"] = "1920x1080,3440x1440"
			want["ratios"] = "16x9,7x5"
		} else {
			want["q"], want["atleast"], want["ratios"] = "", "", ""
		}
		for key, value := range want {
			if q.Get(key) != value {
				t.Errorf("%s: got %q, want %q", key, q.Get(key), value)
			}
		}
		listingReply(w, 1, 1, "", []map[string]any{item("aa0001")})
	})
	params := source.DiscoverParams{Config: json.RawMessage(`{"query":"","categories":"000"}`), Limit: 100}
	page, err := p.Discover(t.Context(), params)
	if err != nil || len(page.Candidates) != 1 {
		t.Fatalf("filterless discovery: %+v %v", page, err)
	}
	params.Config = json.RawMessage(`{"query":"city -cars","categories":"000","tags":["mountains","night sky"]}`)
	params.Filter = &source.FilterHints{
		ExactResolutions: []string{"1920x1080", "3440x1440"},
		Ratios:           []source.Dimensions{{Width: 16, Height: 9}, {Width: 7, Height: 5}},
	}
	page, err = p.Discover(t.Context(), params)
	if err != nil || len(page.Candidates) != 1 {
		t.Fatalf("structured discovery: %+v %v", page, err)
	}
	requireKind(t, p.Validate(t.Context(), json.RawMessage(`{"query":"id:12","tags":["nature"]}`)), "invalid_config")
	requireKind(t, p.Validate(t.Context(), json.RawMessage(`{"tags":["+nature"]}`)), "invalid_config")
}

func TestWallhavenCombinedQueryPaginationAndRetry(t *testing.T) {
	var calls atomic.Int64
	p := fixtureProvider(t, func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		q := r.URL.Query()
		for key, want := range map[string]string{
			"q": `(+mountains | +"night sky") (city | ocean)`, "colors": "0066cc,66cccc",
			"categories": "111", "purity": "100", "atleast": "1920x1080",
			"ratios": "16x9", "sorting": "random", "order": "desc",
		} {
			if got := q.Get(key); got != want {
				t.Errorf("%s: got %q, want %q", key, got, want)
			}
		}
		if call == 1 {
			if q.Get("page") != "1" || q.Has("seed") {
				t.Errorf("unexpected first page: %s", r.URL.RawQuery)
			}
			listingReply(w, 1, 5, "abc123", []map[string]any{item("aa0002"), item("aa0001")})
			return
		}
		if q.Get("page") != "2" || q.Get("seed") != "abc123" {
			t.Errorf("unexpected continuation: %s", r.URL.RawQuery)
		}
		if call == 2 {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		listingReply(w, 2, 5, "abc123", []map[string]any{item("aa0003")})
	})
	config := Config{
		Query: "city | ocean", Tags: []string{"mountains", "night sky"}, TagMatch: "any",
		Colors: []string{"#0066CC", "#0167cd", "#66cccc"}, Categories: "000",
		Sorting: "random", MaxPages: 2,
	}
	params := source.DiscoverParams{
		Config: marshalConfig(t, config), Limit: 100,
		Filter: &source.FilterHints{MinWidth: 1920, MinHeight: 1080, Ratio: &source.Dimensions{Width: 16, Height: 9}},
	}
	page, err := p.Discover(t.Context(), params)
	if err != nil || len(page.Candidates) != 2 || page.Candidates[0].ID != "aa0002" ||
		page.Candidates[1].ID != "aa0001" || source.IsNull(page.NextCursor) || calls.Load() != 1 {
		t.Fatalf("first page: %+v %v after %d calls", page, err, calls.Load())
	}
	params.Cursor = page.NextCursor
	page, err = p.Discover(t.Context(), params)
	typed := requireKind(t, err, "rate_limited")
	if typed.RetryAfterSeconds == nil || *typed.RetryAfterSeconds != 60 || len(page.Candidates) != 0 ||
		!source.IsNull(page.NextCursor) || calls.Load() != 2 {
		t.Fatalf("failed page: %+v %v after %d calls", page, err, calls.Load())
	}
	page, err = p.Discover(t.Context(), params)
	if err != nil || len(page.Candidates) != 1 || page.Candidates[0].ID != "aa0003" ||
		!source.IsNull(page.NextCursor) || calls.Load() != 3 {
		t.Fatalf("retried final page: %+v %v after %d calls", page, err, calls.Load())
	}
}

func TestWallhavenSearchQuery(t *testing.T) {
	tags := []string{"mountains", "night sky"}
	for _, tc := range []struct {
		name   string
		config Config
		want   string
	}{
		{"free text only", Config{Query: "  +space | +stars  "}, "+space | +stars"},
		{"any tags", Config{Tags: tags, TagMatch: "any"}, `+mountains | +"night sky"`},
		{"one any tag", Config{Tags: tags[:1], TagMatch: "any", Query: "city -cars"}, "+mountains city -cars"},
		{"all tags", Config{Tags: tags, TagMatch: "all", Query: "city | ocean"}, `+mountains +"night sky" (city | ocean)`},
		{"literal operators in tag", Config{Tags: []string{"artist (name)", "a|b"}}, `+"artist (name)" +"a|b"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := searchQuery(tc.config); got != tc.want {
				t.Errorf("q = %q, want %q", got, tc.want)
			}
		})
	}
}

func marshalConfig(t *testing.T, config Config) json.RawMessage {
	t.Helper()
	// Leave omitted defaults to the provider, as an external caller would.
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
