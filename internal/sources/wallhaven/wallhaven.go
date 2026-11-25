// Package wallhaven discovers public SFW image metadata. It never downloads
// originals, chooses wallpapers, or writes playback state.
package wallhaven

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/vadim82/panora/internal/source"
)

const (
	maxBodyBytes = 1 << 20
	maxAnyBatch  = 4 // Four upstream pages fit within the service discovery page size.
)

var imageID = regexp.MustCompile(`^[a-z0-9]{6}$`)
var randomSeed = regexp.MustCompile(`^[a-zA-Z0-9]{6}$`)

type Config = source.WallhavenConfig

type Provider struct {
	client  *http.Client
	baseURL string
}

func New() *Provider {
	return &Provider{baseURL: "https://wallhaven.cc/api/v1", client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (*Provider) Describe(context.Context) (source.Description, error) {
	return source.Description{Protocol: source.Protocol, ID: "wallhaven", Name: "Wallhaven", MediaKinds: []string{"image"}, ContentKinds: []string{"https"}, Operations: []string{"describe", "validate", "discover", "resolve"}, FilterHints: []string{"min_width", "min_height", "ratio", "ratios", "exact_resolutions"}, Fields: []source.Field{{Key: "query", Type: "text", Label: "Search"}, {Key: "categories", Type: "text", Label: "Category mask"}, {Key: "tags", Type: "text[]", Label: "Tags"}, {Key: "tag_match", Type: "text", Label: "Tag match"}, {Key: "colors", Type: "text[]", Label: "Colors"}, {Key: "sorting", Type: "text", Label: "Sort order"}, {Key: "order", Type: "text", Label: "Direction"}, {Key: "max_pages", Type: "integer", Label: "Page limit"}, {Key: "top_range", Type: "text", Label: "Toplist period"}}}, nil
}

func parseConfig(ctx context.Context, raw json.RawMessage) (Config, error) {
	c := source.DefaultWallhavenConfig()
	if err := ctx.Err(); err != nil {
		return c, err
	}
	if len(raw) == 0 || source.IsNull(raw) || source.Decode(raw, &c) != nil {
		return c, source.Failure("invalid_config", "expected Wallhaven search settings")
	}
	if err := c.Validate(); err != nil {
		return c, err
	}
	return c, nil
}
func (p *Provider) Validate(ctx context.Context, raw json.RawMessage) error {
	_, err := parseConfig(ctx, raw)
	return err
}

type cursor struct {
	Config    string `json:"config"`
	Page      int    `json:"page"`
	Offset    int    `json:"offset"`
	Branch    int    `json:"branch,omitempty"`
	Seed      string `json:"seed,omitempty"`
	LastPages []int  `json:"last_pages,omitempty"`
}
type reference struct {
	ID string `json:"id"`
}
type wallpaper struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	Purity    string `json:"purity"`
	Path      string `json:"path"`
	MIME      string `json:"file_type"`
	Width     int    `json:"dimension_x"`
	Height    int    `json:"dimension_y"`
	Source    string `json:"source"`
	Created   string `json:"created_at"`
	Views     int64  `json:"views"`
	Favorites int64  `json:"favorites"`
	Uploader  *struct {
		Username string `json:"username"`
		URL      string `json:"url"`
	} `json:"uploader"`
}
type listing struct {
	Data []wallpaper `json:"data"`
	Meta struct {
		Current int    `json:"current_page"`
		Last    int    `json:"last_page"`
		PerPage int    `json:"per_page"`
		Seed    string `json:"seed"`
	} `json:"meta"`
}

func safeURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && len(value) <= 4096 && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && !strings.ContainsAny(value, "\r\n\t ")
}
func candidate(w wallpaper) (source.Candidate, error) {
	var c source.Candidate
	if !imageID.MatchString(w.ID) || w.URL != "https://wallhaven.cc/w/"+w.ID || !safeURL(w.Path) {
		return c, source.Failure("unavailable", "Wallhaven returned invalid image metadata")
	}
	original, _ := url.Parse(w.Path)
	if original.Hostname() != "w.wallhaven.cc" {
		return c, source.Failure("unavailable", "Wallhaven returned an unsupported original location")
	}
	c = source.Candidate{ID: w.ID, MediaKind: "image", Content: &source.Content{MediaKind: "image", Kind: "https", URL: w.Path, MIMEType: w.MIME}, Attribution: &source.Attribution{PageURL: w.URL}}
	c.Ref, _ = json.Marshal(reference{w.ID})
	if w.Width != 0 || w.Height != 0 {
		c.Dimensions = &source.Dimensions{Width: w.Width, Height: w.Height}
	}
	if w.Uploader != nil {
		c.Attribution.Author = w.Uploader.Username
		if safeURL(w.Uploader.URL) {
			c.Attribution.AuthorURL = w.Uploader.URL
		}
	}
	if safeURL(w.Source) {
		c.Attribution.SourceURL = w.Source
	}
	if c.Validate() != nil {
		return source.Candidate{}, source.Failure("unavailable", "Wallhaven returned invalid image metadata")
	}
	return c, nil
}

func (p *Provider) Discover(ctx context.Context, params source.DiscoverParams) (source.Page, error) {
	result := source.Page{Candidates: []source.Candidate{}, NextCursor: json.RawMessage("null")}
	c, err := parseConfig(ctx, params.Config)
	if err != nil {
		return result, err
	}
	if params.Limit < 1 || params.Limit > source.MaxPageSize {
		return result, source.Failure("invalid_config", "limit must be between 1 and 100")
	}
	f := params.Filter
	if !validFilters(f) {
		return result, source.Failure("invalid_config", "invalid image filter hints")
	}
	settings, _ := json.Marshal(struct {
		Config Config
		Filter *source.FilterHints
	}{c, f})
	hash := sha256.Sum256(settings)
	fingerprint := hex.EncodeToString(hash[:])
	matchAny := c.EffectiveTagMatch() == source.WallhavenTagMatchAny && len(c.Tags) > 1
	branches := searchBranches(c, matchAny)
	pageLimit := max(1, c.MaxPages/len(branches))
	batchSize := len(branches)
	if matchAny {
		batchSize = min(batchSize, maxAnyBatch)
	}
	position := cursor{Config: fingerprint, Page: 1, LastPages: make([]int, len(branches))}
	if len(params.Cursor) > source.MaxCursorBytes {
		return result, source.Failure("invalid_cursor", "cursor is too large")
	}
	if len(params.Cursor) != 0 && !source.IsNull(params.Cursor) {
		// Older cursors omit LastPages and index the raw listing; restart from page one.
		position.LastPages = nil
		if source.Decode(params.Cursor, &position) != nil || !position.valid(fingerprint, pageLimit, len(branches), batchSize, c.Sorting == "random") {
			return result, source.Failure("invalid_cursor", "cursor does not match Wallhaven settings")
		}
	}
	categories := c.Categories
	if categories == "" || categories == "000" {
		categories = "111"
	}
	q := url.Values{"categories": {categories}, "purity": {"100"}, "sorting": {c.Sorting}, "order": {c.Order}, "page": {strconv.Itoa(position.Page)}}
	if c.Sorting == "toplist" {
		q.Set("topRange", c.TopRange)
	}
	setGeometryQuery(q, f)
	operation, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	endBranch := min(position.Branch+batchSize, len(branches))
	pages := make([][]rankedCandidate, endBranch-position.Branch)
	for i := position.Branch; i < endBranch; i++ {
		branch := branches[i]
		if position.LastPages[i] > 0 && position.LastPages[i] < position.Page {
			continue
		}
		q.Set("q", branch.query)
		if branch.color != "" {
			q.Set("colors", branch.color)
		}
		if position.Seed != "" {
			q.Set("seed", position.Seed)
		}
		var page listing
		if err := p.get(operation, "/search", q, &page); err != nil {
			if ctx.Err() == nil && operation.Err() != nil {
				return result, source.Failure("unavailable", "Wallhaven search timed out")
			}
			return result, err
		}
		if page.Data == nil || len(page.Data) > 24 || page.Meta.Current != position.Page || page.Meta.Last < position.Page || page.Meta.PerPage != 24 {
			return result, source.Failure("unavailable", "Wallhaven returned invalid pagination metadata")
		}
		if c.Sorting == "random" {
			if !randomSeed.MatchString(page.Meta.Seed) || position.Seed != "" && position.Seed != page.Meta.Seed {
				return result, source.Failure("invalid_cursor", "Wallhaven random seed changed; restart discovery")
			}
			position.Seed = page.Meta.Seed
		}
		position.LastPages[i] = min(page.Meta.Last, pageLimit)
		pageSeen := map[string]bool{}
		for _, w := range page.Data {
			if w.Purity != "sfw" && w.Purity != "sketchy" && w.Purity != "nsfw" || w.MIME == "" {
				return result, source.Failure("unavailable", "Wallhaven returned incomplete image metadata")
			}
			if w.Purity != "sfw" || w.MIME != "image/jpeg" && w.MIME != "image/png" {
				continue
			}
			item, err := candidate(w)
			if err != nil {
				return result, err
			}
			if pageSeen[item.ID] {
				return result, source.Failure("unavailable", "Wallhaven repeated an image ID")
			}
			pageSeen[item.ID] = true
			pages[i-position.Branch] = append(pages[i-position.Branch], rankedCandidate{item, w.Created, w.Views, w.Favorites})
		}
	}
	candidates := mergePages(pages, c, matchAny)
	if position.Offset > len(candidates) {
		return result, source.Failure("unavailable", "Wallhaven changed the current page during discovery")
	}
	end := min(position.Offset+params.Limit, len(candidates))
	result.Candidates = append(result.Candidates, candidates[position.Offset:end]...)
	switch {
	case end < len(candidates):
		position.Offset = end
	case endBranch < len(branches):
		position.Branch = endBranch
		position.Offset = 0
	case slices.ContainsFunc(position.LastPages, func(last int) bool { return last > position.Page }) && position.Page < pageLimit:
		position.Page++
		position.Branch = 0
		position.Offset = 0
	default:
		return result, nil
	}
	result.NextCursor, _ = json.Marshal(position)
	return result, nil
}

func (p cursor) valid(fingerprint string, pages, branches, batchSize int, random bool) bool {
	return p.Config == fingerprint &&
		p.Page >= 1 && p.Page <= pages &&
		p.Branch >= 0 && p.Branch < branches && p.Branch%batchSize == 0 &&
		p.Offset >= 0 && p.Offset < 24*min(batchSize, branches-p.Branch) &&
		len(p.LastPages) == branches &&
		!slices.ContainsFunc(p.LastPages, func(last int) bool { return last < 0 }) &&
		(random && randomSeed.MatchString(p.Seed) || !random && p.Seed == "")
}

type rankedCandidate struct {
	item             source.Candidate
	created          string
	views, favorites int64
}

func mergePages(pages [][]rankedCandidate, c Config, matchAny bool) []source.Candidate {
	var merged []rankedCandidate
	seen := map[string]bool{}
	add := func(row rankedCandidate) {
		if !seen[row.item.ID] {
			seen[row.item.ID] = true
			merged = append(merged, row)
		}
	}
	if matchAny {
		// Relevance, toplist and seeded random have no comparable score in
		// the API, so keep their upstream rank and interleave branches.
		for rank := range 24 {
			for _, page := range pages {
				if rank < len(page) {
					add(page[rank])
				}
			}
		}
		slices.SortStableFunc(merged, func(a, b rankedCandidate) int {
			var comparison int
			switch c.Sorting {
			case "date_added":
				comparison = strings.Compare(a.created, b.created)
			case "views":
				comparison = cmp.Compare(a.views, b.views)
			case "favorites":
				comparison = cmp.Compare(a.favorites, b.favorites)
			}
			if c.Order == "desc" {
				return -comparison
			}
			return comparison
		})
	} else {
		for _, page := range pages {
			for _, row := range page {
				add(row)
			}
		}
	}
	result := make([]source.Candidate, 0, len(merged))
	for _, row := range merged {
		result = append(result, row.item)
	}
	return result
}

type searchBranch struct {
	query, color string
}

func searchBranches(c Config, matchAny bool) []searchBranch {
	tagGroups := [][]string{c.Tags}
	if matchAny {
		tagGroups = make([][]string, 0, len(c.Tags))
		for i := range c.Tags {
			tagGroups = append(tagGroups, c.Tags[i:i+1])
		}
	}
	colors := searchColors(c.Colors)
	branches := make([]searchBranch, 0, len(tagGroups)*len(colors))
	for _, tags := range tagGroups {
		query := searchQuery(c.Query, tags)
		for _, color := range colors {
			branches = append(branches, searchBranch{query, color})
		}
	}
	return branches
}

func searchQuery(query string, tags []string) string {
	var terms []string
	for _, tag := range tags {
		if strings.Contains(tag, " ") {
			tag = `"` + tag + `"`
		}
		terms = append(terms, "+"+tag)
	}
	if query := strings.TrimSpace(query); query != "" {
		terms = append(terms, query)
	}
	return strings.Join(terms, " ")
}

var upstreamColors = strings.Fields("660000 990000 cc0000 cc3333 ea4c88 993399 663399 333399 0066cc 0099cc 66cccc 77cc33 669900 336600 666600 999900 cccc33 ffff00 ffcc33 ff9900 ff6600 cc6633 996633 663300 000000 999999 cccccc ffffff 424153")

func searchColors(selected []string) []string {
	if len(selected) == 0 {
		return []string{""}
	}
	var colors []string
	for _, color := range selected {
		closest := ""
		minimum := 3*255*255 + 1
		rgb, _ := strconv.ParseUint(color[1:], 16, 24)
		for _, option := range upstreamColors {
			target, _ := strconv.ParseUint(option, 16, 24)
			distance := 0
			for _, shift := range []int{16, 8, 0} {
				delta := int((rgb>>shift)&255) - int((target>>shift)&255)
				distance += delta * delta
			}
			if distance < minimum {
				closest, minimum = option, distance
			}
		}
		if !slices.Contains(colors, closest) {
			colors = append(colors, closest)
		}
	}
	return colors
}

func validFilters(f *source.FilterHints) bool {
	if f == nil {
		return true
	}
	if f.MinWidth < 0 || f.MinHeight < 0 || len(f.ExactResolutions) > source.MaxExactResolutions || len(f.Ratios) > 14 {
		return false
	}
	if f.Ratio != nil && (f.Ratio.Width < 1 || f.Ratio.Height < 1 || len(f.Ratios) > 0) {
		return false
	}
	for _, ratio := range f.Ratios {
		if ratio.Width < 1 || ratio.Height < 1 {
			return false
		}
	}
	for _, resolution := range f.ExactResolutions {
		if _, valid := source.ParseExactResolution(resolution); !valid {
			return false
		}
	}
	return true
}

func setGeometryQuery(q url.Values, f *source.FilterHints) {
	if f == nil {
		return
	}
	if len(f.ExactResolutions) > 0 {
		q.Set("resolutions", strings.Join(f.ExactResolutions, ","))
	} else if f.MinWidth > 0 && f.MinHeight > 0 && (f.MinWidth > 1 || f.MinHeight > 1) {
		q.Set("atleast", fmt.Sprintf("%dx%d", f.MinWidth, f.MinHeight))
	}
	ratios := f.Ratios
	if f.Ratio != nil {
		ratios = []source.Dimensions{*f.Ratio}
	}
	if len(ratios) > 0 {
		values := make([]string, 0, len(ratios))
		for _, ratio := range ratios {
			values = append(values, fmt.Sprintf("%dx%d", ratio.Width, ratio.Height))
		}
		q.Set("ratios", strings.Join(values, ","))
	}
}

func (p *Provider) Resolve(ctx context.Context, params source.ResolveParams) (source.Resolved, error) {
	var result source.Resolved
	if _, err := parseConfig(ctx, params.Config); err != nil {
		return result, err
	}
	var ref reference
	if !imageID.MatchString(params.ID) || source.Decode(params.Ref, &ref) != nil || ref.ID != params.ID {
		return result, source.Failure("invalid_config", "candidate ID does not match its Wallhaven reference")
	}
	var reply struct {
		Data wallpaper `json:"data"`
	}
	if err := p.get(ctx, "/w/"+params.ID, nil, &reply); err != nil {
		return result, err
	}
	if reply.Data.ID != params.ID {
		return result, source.Failure("unavailable", "Wallhaven resolved a different image")
	}
	if reply.Data.Purity != "sfw" || reply.Data.MIME != "image/jpeg" && reply.Data.MIME != "image/png" {
		return result, source.Failure("unsupported", "Wallhaven image is not supported SFW content")
	}
	c, err := candidate(reply.Data)
	if err != nil {
		return result, err
	}
	return source.Resolved{Content: *c.Content, Dimensions: c.Dimensions, Attribution: c.Attribution}, nil
}

func retryHint(value string) *int64 { return source.RetryAfter(value, time.Now()) }
func (p *Provider) get(ctx context.Context, path string, query url.Values, target any) error {
	endpoint := p.baseURL + path
	if len(query) != 0 {
		endpoint += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return source.Failure("internal", "could not create Wallhaven metadata request")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Panora/0.1 (wallpaper metadata)")
	response, err := p.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return source.Failure("unavailable", "Wallhaven metadata request failed")
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case 200:
	case 401:
		return source.Failure("auth_required", "Wallhaven requires authentication; credentials are not supported yet")
	case 403:
		return source.Failure("auth_rejected", "Wallhaven denied this metadata request")
	case 404:
		return source.Failure("not_found", "Wallhaven image or page is unavailable")
	case 429:
		return &source.Error{Code: "rate_limited", Message: "Wallhaven request limit reached", RetryAfterSeconds: retryHint(response.Header.Get("Retry-After"))}
	default:
		if response.StatusCode >= 500 {
			return &source.Error{Code: "unavailable", Message: "Wallhaven is temporarily unavailable", RetryAfterSeconds: retryHint(response.Header.Get("Retry-After"))}
		}
		return source.Failure("unavailable", "Wallhaven rejected the metadata request")
	}
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return source.Failure("unavailable", "Wallhaven returned non-JSON metadata")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return source.Failure("unavailable", "could not read Wallhaven metadata")
	}
	if len(data) > maxBodyBytes {
		return source.Failure("unavailable", "Wallhaven metadata exceeds the size limit")
	}
	if json.Unmarshal(data, target) != nil {
		return source.Failure("unavailable", "Wallhaven returned malformed metadata")
	}
	return nil
}
