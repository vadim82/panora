// Package alphacoders implements the Alpha Coders 4.0 desktop wallpaper API.
package alphacoders

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vadim82/panora/internal/localfiles"
	"github.com/vadim82/panora/internal/source"
)

const (
	id        = source.AlphaCodersID
	pageSize  = 30
	scanPages = 3
)

type Tag = source.AlphaTag

type settings struct {
	Tags    []Tag  `json:"tags,omitempty"`
	Keyword string `json:"keyword,omitempty"`
	Sort    string `json:"sort,omitempty"`
}

type item struct {
	ID       int64  `json:"content_id"`
	Title    string `json:"name"`
	Type     string `json:"file_type"`
	Checksum string `json:"md5_checksum"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Original string `json:"src"`
	Thumb    string `json:"thumb"`
	Page     string `json:"url_page"`
}

type response struct {
	Success  bool   `json:"success"`
	Error    string `json:"error"`
	Window   string `json:"window"`
	Contents []item `json:"contents"`
	Content  item   `json:"content"`
	Tags     []Tag  `json:"tags"`
}

type Provider struct {
	credential string
	base       string
	client     *http.Client
	mu         sync.Mutex
	retry      time.Time
}

func New(credential string) *Provider {
	return &Provider{
		credential: credential, base: "https://api.alphacoders.com/4.0",
		client: &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

func (p *Provider) Describe(context.Context) (source.Description, error) {
	return source.Description{Protocol: source.Protocol, ID: id, Name: "Alpha Coders",
		MediaKinds: []string{"image"}, ContentKinds: []string{"https"},
		Operations:  []string{"describe", "validate", "discover", "resolve"},
		FilterHints: []string{"min_width", "min_height", "exact_resolutions", "ratio_range"}}, nil
}

func readSettings(raw json.RawMessage) (settings, error) {
	var cfg settings
	if source.Decode(raw, &cfg) != nil || len(cfg.Tags) > 5 || len(cfg.Keyword) > 128 ||
		len(cfg.Tags) > 0 && strings.TrimSpace(cfg.Keyword) != "" ||
		cfg.Sort != "" && cfg.Sort != "newest" && cfg.Sort != "highest_rated" {
		return cfg, source.Failure("invalid_settings", "use up to five approved tags or a keyword and a supported sort order")
	}
	for i, tag := range cfg.Tags {
		if tag.ID <= 0 || strings.TrimSpace(tag.Name) == "" || len(tag.Name) > 128 || strings.ContainsAny(tag.Name, "\x00\r\n") ||
			slices.ContainsFunc(cfg.Tags[:i], func(previous Tag) bool { return previous.ID == tag.ID }) {
			return cfg, source.Failure("invalid_settings", "choose distinct approved Alpha Coders tags")
		}
	}
	if cfg.Sort == "" {
		cfg.Sort = "newest"
	}
	return cfg, nil
}

func (p *Provider) Validate(_ context.Context, raw json.RawMessage) error {
	_, err := readSettings(raw)
	return err
}

func (p *Provider) key() (string, error) {
	before, err := os.Lstat(p.credential)
	if errors.Is(err, os.ErrNotExist) {
		return "", source.Failure("auth_required", "connect a personal Alpha Coders API key")
	}
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0077 != 0 {
		return "", source.Failure("unavailable", "the Alpha Coders credential must be a private regular file")
	}
	file, err := localfiles.OpenRegular(p.credential)
	if err != nil {
		return "", source.Failure("unavailable", "cannot open the Alpha Coders credential")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !os.SameFile(before, info) || info.Mode().Perm()&0077 != 0 {
		return "", source.Failure("unavailable", "the Alpha Coders credential must be a private regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, 257))
	key := strings.TrimSpace(string(data))
	if err != nil || !keyPattern.MatchString(key) {
		return "", source.Failure("auth_rejected", "the saved Alpha Coders credential is invalid")
	}
	return key, nil
}

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)

func (p *Provider) Connected() bool { _, err := p.key(); return err == nil }

func (p *Provider) SetKey(ctx context.Context, key string, remove bool) error {
	if remove {
		p.mu.Lock()
		defer p.mu.Unlock()
		if err := os.Remove(p.credential); err != nil && !errors.Is(err, os.ErrNotExist) {
			return source.Failure("unavailable", "could not remove the Alpha Coders key")
		}
		p.retry = time.Time{}
		return nil
	}
	key = strings.TrimSpace(key)
	if !keyPattern.MatchString(key) {
		return source.Failure("invalid_settings", "enter a personal Alpha Coders API key")
	}
	if _, err := p.call(ctx, key, "/query-count", nil, false); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := writeKey(p.credential, key); err != nil {
		return source.Failure("unavailable", "could not store the Alpha Coders key")
	}
	p.retry = time.Time{}
	return nil
}

func writeKey(path, key string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".alpha-coders-key-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, err = file.WriteString(key + "\n")
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), path)
}

func (p *Provider) get(ctx context.Context, endpoint string, query url.Values) (response, error) {
	key, err := p.key()
	if err != nil {
		return response{}, err
	}
	return p.call(ctx, key, endpoint, query, true)
}

func (p *Provider) call(ctx context.Context, key, endpoint string, query url.Values, gate bool) (response, error) {
	var result response
	if gate {
		p.mu.Lock()
		until := p.retry
		p.mu.Unlock()
		if delay := time.Until(until); delay > 0 {
			seconds := int64(math.Ceil(delay.Seconds()))
			return result, &source.Error{Code: "rate_limited", Message: "Alpha Coders is temporarily rate limited", RetryAfterSeconds: &seconds}
		}
	}
	params := url.Values{"auth": {key}}
	for name, values := range query {
		params[name] = values
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.base+endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return result, source.Failure("unavailable", "could not build the Alpha Coders request")
	}
	resp, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, source.Failure("unavailable", "could not reach Alpha Coders")
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, source.MaxResponseBytes+1))
	parsed := readErr == nil && len(raw) <= source.MaxResponseBytes && json.Unmarshal(raw, &result) == nil
	switch {
	case resp.StatusCode == 429 || result.Error == "rate_limit":
		now := time.Now()
		hint := source.RetryAfter(resp.Header.Get("Retry-After"), now)
		if hint == nil {
			seconds := int64(60)
			if result.Window == "day" {
				seconds = int64(math.Ceil(now.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour).Sub(now).Seconds()))
			}
			hint = &seconds
		}
		if gate {
			p.mu.Lock()
			until := now.Add(time.Duration(*hint) * time.Second)
			if until.After(p.retry) {
				p.retry = until
			}
			p.mu.Unlock()
		}
		return response{}, &source.Error{Code: "rate_limited", Message: "Alpha Coders request limit reached", RetryAfterSeconds: hint}
	case resp.StatusCode == 401 || resp.StatusCode == 403 || result.Error == "invalid_auth":
		return response{}, source.Failure("auth_rejected", "Alpha Coders rejected the API key")
	case resp.StatusCode == 404 || result.Error == "not_found":
		return response{}, source.Failure("not_found", "Alpha Coders wallpaper is no longer available")
	case resp.StatusCode != 200 || !parsed || !result.Success:
		return response{}, source.Failure("unavailable", "Alpha Coders could not complete the request")
	}
	return result, nil
}

type search struct{ name, endpoint, keyword string }

func searches(cfg settings) []search {
	if len(cfg.Tags) > 0 {
		result := make([]search, 0, len(cfg.Tags))
		for _, tag := range cfg.Tags {
			result = append(result, search{name: tag.Name, endpoint: "/tags/" + strconv.FormatInt(tag.ID, 10)})
		}
		return result
	}
	if keyword := strings.TrimSpace(cfg.Keyword); keyword != "" {
		return []search{{name: keyword, endpoint: "/search", keyword: keyword}}
	}
	return []search{{name: "All wallpapers", endpoint: "/contents"}}
}

func queryFor(filter *source.FilterHints, sort string, page int, keyword string) url.Values {
	query := url.Values{"content_type": {"wallpaper"}, "sort": {sort}, "page": {strconv.Itoa(page)}}
	if keyword != "" {
		query.Set("term", keyword)
	}
	if filter == nil {
		return query
	}
	width, height := filter.MinWidth, filter.MinHeight
	operator := "min"
	if len(filter.ExactResolutions) == 1 {
		if size, ok := source.ParseExactResolution(filter.ExactResolutions[0]); ok {
			width, height, operator = size.Width, size.Height, "equal"
		}
	}
	if width > 0 && height > 0 {
		query.Set("width", strconv.Itoa(width))
		query.Set("height", strconv.Itoa(height))
		query.Set("operator", operator)
	}
	if bounds := filter.RatioRange; bounds != nil {
		minimum := math.Floor(bounds.Min*1e6) / 1e6
		maximum := math.Ceil(bounds.Max*1e6) / 1e6
		query.Set("min_ratio", strconv.FormatFloat(minimum, 'f', 6, 64))
		query.Set("max_ratio", strconv.FormatFloat(maximum, 'f', 6, 64))
	}
	return query
}

type cursor struct {
	Search int                 `json:"search"`
	Page   int                 `json:"page"`
	Offset int                 `json:"offset"`
	Filter *source.FilterHints `json:"filter,omitempty"`
}

func (p *Provider) Discover(ctx context.Context, params source.DiscoverParams) (source.Page, error) {
	cfg, err := readSettings(params.Config)
	if err != nil {
		return source.Page{}, err
	}
	queries := searches(cfg)
	state := cursor{Page: 1, Filter: params.Filter}
	defaultCursor := len(params.Cursor) == 0 || source.IsNull(params.Cursor)
	if !defaultCursor {
		if source.Decode(params.Cursor, &state) != nil || state.Search < 0 || state.Search >= len(queries) ||
			state.Page < 1 || state.Page > scanPages || state.Offset < 0 || state.Offset >= pageSize ||
			!sameFilter(state.Filter, params.Filter) {
			return source.Page{}, source.Failure("invalid_cursor", "restart the Alpha Coders search")
		}
	}
	if params.Limit < 1 || params.Limit > source.MaxPageSize {
		return source.Page{}, source.Failure("invalid_settings", "invalid Alpha Coders page limit")
	}
	target := queries[state.Search]
	result, err := p.get(ctx, target.endpoint, queryFor(params.Filter, cfg.Sort, state.Page, target.keyword))
	if err != nil {
		return source.Page{}, err
	}
	if len(result.Contents) > pageSize {
		return source.Page{}, source.Failure("unavailable", "Alpha Coders returned an invalid page")
	}
	page := source.Page{Candidates: []source.Candidate{}, NextCursor: json.RawMessage("null")}
	seen := map[int64]bool{}
	for state.Offset < len(result.Contents) && len(page.Candidates) < params.Limit {
		record := result.Contents[state.Offset]
		state.Offset++
		if candidate, ok := record.candidate(); ok && !seen[record.ID] {
			seen[record.ID] = true
			page.Candidates = append(page.Candidates, candidate)
		}
	}
	if state.Offset >= len(result.Contents) {
		state.Offset = 0
		if len(result.Contents) == pageSize && state.Page < scanPages {
			state.Page++
		} else {
			state.Search++
			state.Page = 1
		}
	}
	if state.Search < len(queries) {
		page.NextCursor, _ = json.Marshal(state)
	}
	return page, nil
}

func sameFilter(a, b *source.FilterHints) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

var imageHost = regexp.MustCompile(`^images[0-9]*\.alphacoders\.com$`)

func imageURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u != nil && u.Scheme == "https" && imageHost.MatchString(u.Host) && u.User == nil &&
		u.Path != "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && len(raw) <= 4096
}

func (x item) candidate() (source.Candidate, bool) {
	if x.ID <= 0 || x.Width <= 0 || x.Height <= 0 || !imageURL(x.Original) ||
		!slices.Contains([]string{"jpg", "jpeg", "png", "webp"}, x.Type) {
		return source.Candidate{}, false
	}
	id := strconv.FormatInt(x.ID, 10)
	c := source.Candidate{ID: id, Ref: json.RawMessage(id), MediaKind: "image",
		Dimensions:  &source.Dimensions{Width: x.Width, Height: x.Height},
		Attribution: &source.Attribution{Title: x.Title, PageURL: source.AlphaCodersPage(x.Page), LicenseURL: "https://alphacoders.com/site/using-content"}}
	return c, c.Validate() == nil
}

func (p *Provider) Resolve(ctx context.Context, params source.ResolveParams) (source.Resolved, error) {
	itemID, err := strconv.ParseInt(params.ID, 10, 64)
	if err != nil || itemID <= 0 || string(params.Ref) != params.ID {
		return source.Resolved{}, source.Failure("invalid_settings", "invalid Alpha Coders wallpaper identity")
	}
	result, err := p.get(ctx, "/contents/"+params.ID, nil)
	if err != nil {
		return source.Resolved{}, err
	}
	x := result.Content
	candidate, ok := x.candidate()
	if !ok || x.ID != itemID {
		return source.Resolved{}, source.Failure("not_found", "Alpha Coders no longer offers this still image")
	}
	return source.Resolved{Content: source.Content{Kind: "https", MediaKind: "image", URL: x.Original}, Dimensions: candidate.Dimensions, Attribution: candidate.Attribution}, nil
}

func (p *Provider) Tags(ctx context.Context, prefix string) ([]Tag, error) {
	prefix = strings.TrimSpace(prefix)
	if len(prefix) < 2 || len(prefix) > 128 {
		return nil, source.Failure("invalid_settings", "enter a tag prefix between 2 and 128 bytes")
	}
	result, err := p.get(ctx, "/tags", url.Values{"term": {prefix}, "content_type": {"wallpaper"}})
	if err != nil {
		return nil, err
	}
	tags := make([]Tag, 0, 10)
	for _, tag := range result.Tags {
		if tag.ID > 0 && tag.Total > 0 && strings.TrimSpace(tag.Name) != "" && len(tag.Name) <= 128 && !strings.ContainsAny(tag.Name, "\x00\r\n") {
			tags = append(tags, tag)
			if len(tags) == 10 {
				break
			}
		}
	}
	return tags, nil
}

func (p *Provider) Check(ctx context.Context, raw json.RawMessage, filter *source.FilterHints, eligible func(source.Dimensions) bool) (source.AlphaCheck, error) {
	cfg, err := readSettings(raw)
	if err != nil {
		return source.AlphaCheck{}, err
	}
	if _, err = p.key(); err != nil {
		return source.AlphaCheck{}, err
	}
	result := source.AlphaCheck{Scopes: []source.AlphaScope{}, Samples: []source.AlphaSample{}}
	groups := [][]source.AlphaSample{}
	seen := map[string]bool{}
	targets := searches(cfg)
	for i, target := range targets {
		scope := source.AlphaScope{Name: target.name}
		group := []source.AlphaSample{}
		local := map[string]bool{}
		for page := 1; page <= 2; page++ {
			data, err := p.get(ctx, target.endpoint, queryFor(filter, cfg.Sort, page, target.keyword))
			if err != nil {
				var typed *source.Error
				if !errors.As(err, &typed) {
					return source.AlphaCheck{}, err
				}
				scope.Error = typed.Message
				scope.More = true
				break
			}
			scope.Pages++
			scope.More = len(data.Contents) >= pageSize
			for _, x := range data.Contents[:min(len(data.Contents), pageSize)] {
				candidate, ok := x.candidate()
				if !ok || !eligible(*candidate.Dimensions) {
					continue
				}
				key := x.Checksum
				if key == "" {
					key = candidate.ID
				}
				local[key] = true
				if seen[key] {
					continue
				}
				seen[key] = true
				if thumbnailURL(x.Thumb) {
					title := strings.ToValidUTF8(x.Title[:min(len(x.Title), 256)], "")
					group = append(group, source.AlphaSample{Title: title, Width: x.Width, Height: x.Height, Thumbnail: x.Thumb, Page: candidate.Attribution.PageURL})
				}
			}
			if !scope.More {
				break
			}
		}
		scope.Count = len(local)
		result.Scopes = append(result.Scopes, scope)
		groups = append(groups, group)
		if scope.Error != "" {
			for _, skipped := range targets[i+1:] {
				result.Scopes = append(result.Scopes, source.AlphaScope{Name: skipped.name, More: true})
			}
			break
		}
	}
	result.Count = len(seen)
	for row := 0; row < 2*pageSize; row++ {
		for _, group := range groups {
			if len(group) > row {
				result.Samples = append(result.Samples, group[row])
			}
		}
	}
	return result, nil
}

var thumbPath = regexp.MustCompile(`^/[0-9]{1,20}/thumb-350-[0-9]{1,20}\.(webp|jpg|jpeg|png)$`)

func thumbnailURL(raw string) bool {
	if !imageURL(raw) {
		return false
	}
	u, _ := url.Parse(raw)
	return thumbPath.MatchString(u.EscapedPath())
}

// Thumbnail retrieves only 350-pixel public preview assets, with no API key.
// This keeps remote image origins out of the native webview CSP.
func (p *Provider) Thumbnail(ctx context.Context, raw string) (string, error) {
	if !thumbnailURL(raw) {
		return "", source.Failure("invalid_settings", "invalid Alpha Coders preview URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", source.Failure("unavailable", "preview unavailable")
	}
	res, err := p.client.Do(req)
	if err != nil {
		return "", source.Failure("unavailable", "preview unavailable")
	}
	defer res.Body.Close()
	const size = 192 << 10
	data, err := io.ReadAll(io.LimitReader(res.Body, size+1))
	if err != nil || res.StatusCode != 200 || len(data) > size {
		return "", source.Failure("unavailable", "preview unavailable")
	}
	mime := http.DetectContentType(data)
	if mime != "image/jpeg" && mime != "image/png" && mime != "image/webp" {
		return "", source.Failure("unavailable", "preview format unavailable")
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}
