package wallhaven

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/catalog"
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

func TestWallhavenColorUnionAndPaginationBudget(t *testing.T) {
	var requests []string
	p := fixtureProvider(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		color, page := q.Get("colors"), q.Get("page")
		requests = append(requests, color+"/"+page)
		switch color + "/" + page {
		case "0066cc/1":
			listingReply(w, 1, 1, "abc123", []map[string]any{item("aa0001"), item("aa0002")})
		case "66cccc/1":
			if q.Get("seed") != "abc123" {
				t.Errorf("random seed lost across colors: %s", q.Get("seed"))
			}
			listingReply(w, 1, 3, "abc123", []map[string]any{item("aa0002"), item("aa0003")})
		case "66cccc/2":
			listingReply(w, 2, 3, "abc123", []map[string]any{item("aa0004")})
		default:
			t.Errorf("unexpected fanout request: %s", r.URL.RawQuery)
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	})
	params := source.DiscoverParams{
		Config: json.RawMessage(`{"query":"","categories":"000","colors":["#0066CC","#0167cd","#66cccc"],"max_pages":4,"sorting":"random"}`),
		Limit:  100,
	}
	page, err := p.Discover(t.Context(), params)
	if err != nil || len(page.Candidates) != 3 || page.Candidates[2].ID != "aa0003" || source.IsNull(page.NextCursor) {
		t.Fatalf("color union: %+v %v", page, err)
	}
	params.Cursor = page.NextCursor
	changed := params
	changed.Config = json.RawMessage(`{"query":"","categories":"000","colors":["#424153"],"max_pages":4,"sorting":"random"}`)
	_, err = p.Discover(t.Context(), changed)
	requireKind(t, err, "invalid_cursor")
	page, err = p.Discover(t.Context(), params)
	if err != nil || len(page.Candidates) != 1 || page.Candidates[0].ID != "aa0004" || !source.IsNull(page.NextCursor) || len(requests) != 3 {
		t.Fatalf("bounded fanout: %+v, requests: %v, err: %v", page, requests, err)
	}
}

func TestWallhavenTagUnionAndIntersection(t *testing.T) {
	shared, mountains, coast := item("aa0001"), item("aa0002"), item("aa0003")
	shared["views"], mountains["views"], coast["views"] = 2, 1, 3
	var calls atomic.Int64
	p := fixtureProvider(t, func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		q := r.URL.Query()
		for key, value := range map[string]string{
			"categories": "101", "purity": "100", "atleast": "1920x1080",
			"ratios": "16x9", "sorting": "views", "order": "desc",
		} {
			if q.Get(key) != value {
				t.Errorf("%s: got %q, want %q", key, q.Get(key), value)
			}
		}
		switch q.Get("q") + "/" + q.Get("colors") + "/" + q.Get("page") {
		case "+mountains city -cars/0066cc/1":
			listingReply(w, 1, 1, "", []map[string]any{shared, mountains})
		case "+mountains city -cars/66cccc/1":
			listingReply(w, 1, 1, "", []map[string]any{mountains})
		case "+coast city -cars/0066cc/1":
			if call == 3 {
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			listingReply(w, 1, 2, "", []map[string]any{coast, shared})
		case "+coast city -cars/0066cc/2":
			listingReply(w, 2, 2, "", []map[string]any{item("aa0004")})
		case "+coast city -cars/66cccc/1", "+mountains +coast city -cars/0066cc/1":
			listingReply(w, 1, 1, "", []map[string]any{shared})
		case "+mountains +coast city -cars/66cccc/1":
			listingReply(w, 1, 1, "", []map[string]any{})
		default:
			t.Errorf("unexpected branch: %s", r.URL.RawQuery)
			w.WriteHeader(http.StatusBadRequest)
		}
	})
	config := Config{
		Tags: []string{"mountains", "coast"}, TagMatch: "any",
		Colors: []string{"#0066cc", "#66cccc"}, Query: "city -cars",
		Categories: "101", Sorting: "views", Order: "desc", MaxPages: 8,
	}
	params := source.DiscoverParams{
		Config: marshalConfig(t, config), Limit: 2,
		Filter: &source.FilterHints{MinWidth: 1920, MinHeight: 1080, Ratio: &source.Dimensions{Width: 16, Height: 9}},
	}
	page, err := p.Discover(t.Context(), params)
	typed := requireKind(t, err, "rate_limited")
	if typed.RetryAfterSeconds == nil || *typed.RetryAfterSeconds != 60 || len(page.Candidates) != 0 || !source.IsNull(page.NextCursor) || calls.Load() != 3 {
		t.Fatalf("failed branch published partial data or retried: %+v %v after %d calls", page, typed, calls.Load())
	}
	page, err = p.Discover(t.Context(), params)
	if err != nil || len(page.Candidates) != 2 || page.Candidates[0].ID != coast["id"] ||
		page.Candidates[1].ID != shared["id"] || source.IsNull(page.NextCursor) || calls.Load() != 7 {
		t.Fatalf("first union page: %+v %v after %d calls", page, err, calls.Load())
	}
	params.Cursor = page.NextCursor
	page, err = p.Discover(t.Context(), params)
	if err != nil || len(page.Candidates) != 1 || page.Candidates[0].ID != mountains["id"] || source.IsNull(page.NextCursor) {
		t.Fatalf("union continuation: %+v %v", page, err)
	}
	params.Cursor = page.NextCursor
	page, err = p.Discover(t.Context(), params)
	if err != nil || len(page.Candidates) != 1 || page.Candidates[0].ID != "aa0004" || !source.IsNull(page.NextCursor) || calls.Load() != 12 {
		t.Fatalf("only the unexhausted tag/color branch should advance: %+v %v after %d calls", page, err, calls.Load())
	}
	config.TagMatch = "all"
	params.Config = marshalConfig(t, config)
	_, err = p.Discover(t.Context(), params)
	requireKind(t, err, "invalid_cursor")
	params.Cursor = nil
	page, err = p.Discover(t.Context(), params)
	if err != nil || len(page.Candidates) != 1 || page.Candidates[0].ID != shared["id"] || !source.IsNull(page.NextCursor) {
		t.Fatalf("intersection: %+v %v", page, err)
	}
}

func TestWallhavenAnyBranchBatchBudget(t *testing.T) {
	const failureCall = maxAnyBatch + 2
	var calls atomic.Int64
	p := fixtureProvider(t, func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		q := r.URL.Query()
		if q.Get("page") != "1" || call > 1 && q.Get("seed") != "abc123" {
			t.Errorf("unexpected Any request: %s", r.URL.RawQuery)
		}
		branchCall := call
		switch {
		case call == failureCall:
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		case call > failureCall:
			branchCall -= 2 // The successful retry starts the second batch again.
		}
		listingReply(w, 1, 10, "abc123", []map[string]any{item(fmt.Sprintf("ab%04d", branchCall)), item("shared")})
	})
	config := Config{
		TagMatch: "any", Sorting: "random", MaxPages: 1,
		Colors: []string{"#660000", "#cc0000", "#000000", "#ffffff", "#0099cc", "#cc6633"},
	}
	for tag := range source.MaxWallhavenTags {
		config.Tags = append(config.Tags, fmt.Sprintf("tag%d", tag))
	}
	params := source.DiscoverParams{Config: marshalConfig(t, config), Limit: 100}
	store, err := catalog.Open(t.Context(), filepath.Join(t.TempDir(), "scan.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scan, err := store.StartScan(t.Context(), "wallhaven", "search", "r1", false)
	if err != nil {
		t.Fatal(err)
	}
	branches := len(config.Tags) * len(config.Colors)
	batches := branches / maxAnyBatch
	extraCalls := 0
	for batch := 1; batch <= batches; batch++ {
		if batch == 2 {
			failedPage, err := p.Discover(t.Context(), params)
			requireKind(t, err, "rate_limited")
			checkpoint, err := store.StartScan(t.Context(), "wallhaven", "search", "r1", false)
			if err != nil || !bytes.Equal(checkpoint.Cursor, params.Cursor) || len(failedPage.Candidates) != 0 || calls.Load() != failureCall {
				t.Fatalf("failed batch changed its checkpoint or exposed candidates: %+v %+v %v", checkpoint, failedPage, err)
			}
			extraCalls = 2
		}
		page, err := p.Discover(t.Context(), params)
		if err != nil || len(page.Candidates) != maxAnyBatch+1 || len(page.NextCursor) > source.MaxCursorBytes ||
			calls.Load() != int64(batch*maxAnyBatch+extraCalls) || source.IsNull(page.NextCursor) != (batch == batches) {
			t.Fatalf("batch %d: %+v %v after %d calls", batch, page, err, calls.Load())
		}
		if _, err := store.CommitPage(t.Context(), scan, "", page); err != nil {
			t.Fatal(err)
		}
		params.Cursor, scan.Cursor = page.NextCursor, page.NextCursor
	}
	committed, err := store.Candidates(t.Context(), "wallhaven", "search", "r1", branches+1)
	if err != nil || len(committed) != branches+1 {
		t.Fatalf("cross-batch candidate IDs were not merged: %d candidates after %d requests: %v", len(committed), calls.Load(), err)
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
