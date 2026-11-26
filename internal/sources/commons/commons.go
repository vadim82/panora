// Package commons searches Wikimedia Commons and discovers approved files
// without downloading originals or changing playback state.
package commons

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/vadim82/panora/internal/source"
)

const (
	Name       = "Wikimedia Commons"
	Collection = "Featured landscapes"
	maxBatch   = 12
)

// Each entry was reviewed for safe-for-work imagery, correct credit, and reuse
// terms. The original digest covers the interval between resolution and download.
//
//go:embed featured-landscapes.json
var reviewedData []byte

type reviewedFile struct {
	PageID      int    `json:"page_id"`
	SHA1        string `json:"sha1"`
	SHA256      string `json:"sha256"`
	TermsSHA256 string `json:"terms_sha256"`
	Credit      string `json:"credit"`
}

func (file reviewedFile) id() string { return strconv.Itoa(file.PageID) + "-" + file.SHA1 }

type Provider struct {
	endpoint          string
	client            *http.Client
	files             []reviewedFile
	collection        string
	searchSlot        chan struct{}
	searchRetryUntil  time.Time // guarded by searchSlot
	suggestSlot       chan struct{}
	suggestRetryUntil time.Time // guarded by suggestSlot
}

func New() *Provider {
	return newProvider("https://commons.wikimedia.org/w/api.php", reviewedData)
}

func newProvider(endpoint string, reviewed []byte) *Provider {
	var files []reviewedFile
	if err := source.Decode(reviewed, &files); err != nil {
		panic("invalid reviewed Commons collection: " + err.Error())
	}
	hash := sha256.Sum256(reviewed)
	return &Provider{
		endpoint: endpoint,
		client: &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		files: files, collection: hex.EncodeToString(hash[:]), searchSlot: make(chan struct{}, 1), suggestSlot: make(chan struct{}, 1),
	}
}

func (*Provider) Describe(context.Context) (source.Description, error) {
	return source.Description{
		Protocol: source.Protocol, ID: source.CommonsID, Name: Name,
		MediaKinds: []string{"image"}, ContentKinds: []string{"https"},
		Operations: []string{"describe", "validate", "discover", "resolve", source.SuggestMethod},
		Fields:     []source.Field{{Key: "collection", Type: "text", Label: "Collection"}},
	}, nil
}

func Editor() source.EditorManifest {
	return source.EditorManifest{
		Version: source.EditorVersion,
		Groups: []source.EditorGroup{{ID: "collection", Fields: []source.EditorField{{
			Key: "collection", Label: "Collection", Control: "select", Required: true,
			Help:    "Panora contacts Wikimedia Commons to find and download wallpapers. Search results enter rotation after approval.",
			Default: json.RawMessage(`"search"`),
			Options: []source.EditorOption{{Label: "Search Commons", Value: source.CommonsLive}, {Label: Collection, Value: source.CommonsFeatured}},
		}, {
			Key: "category", Label: "Commons categories", Control: "multivalue", Suggest: source.SuggestMethod,
		}}}},
	}
}

func (*Provider) Validate(ctx context.Context, raw json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := source.DecodeCommonsConfig(raw)
	return err
}

type position struct {
	Collection string `json:"collection"`
	Offset     int    `json:"offset"`
}

func (p *Provider) Discover(ctx context.Context, params source.DiscoverParams) (source.Page, error) {
	page := source.Page{Candidates: []source.Candidate{}, NextCursor: json.RawMessage("null")}
	cfg, err := source.DecodeCommonsConfig(params.Config)
	if err != nil {
		return page, err
	}
	if params.Limit < 1 || params.Limit > source.MaxPageSize {
		return page, source.Failure("invalid_config", "invalid Commons page limit")
	}
	live := cfg.Collection == source.CommonsLive
	files, collection := p.files, p.collection
	if live {
		files = make([]reviewedFile, len(cfg.Approved))
		for i, approved := range cfg.Approved {
			files[i] = reviewedFile{PageID: approved.PageID}
		}
		hash := sha256.Sum256(params.Config)
		collection = hex.EncodeToString(hash[:])
	}
	pos := position{Collection: collection}
	if len(params.Cursor) != 0 && !source.IsNull(params.Cursor) {
		if len(params.Cursor) > source.MaxCursorBytes || source.Decode(params.Cursor, &pos) != nil ||
			pos.Collection != collection || pos.Offset < 1 || pos.Offset >= len(files) {
			return page, source.Failure("invalid_cursor", "cursor does not match the reviewed collection")
		}
	}
	end := min(len(files), pos.Offset+min(params.Limit, maxBatch))
	items, err := p.fetch(ctx, files[pos.Offset:end])
	if err != nil {
		return page, err
	}
	for i := pos.Offset; i < end; i++ {
		pin := files[i]
		if live {
			item := items[pin.PageID]
			if c, approval, ok := liveCandidate(item); ok && approval == cfg.Approved[i] && cfg.Allows(c) {
				page.Candidates = append(page.Candidates, c)
			}
		} else if c, ok := acceptedCandidate(pin, items[pin.PageID]); ok {
			page.Candidates = append(page.Candidates, c)
		}
	}
	if end < len(files) {
		page.NextCursor, _ = json.Marshal(position{collection, end})
	}
	return page, nil
}

type reference struct {
	PageID int `json:"page_id"`
}

func (p *Provider) Resolve(ctx context.Context, params source.ResolveParams) (source.Resolved, error) {
	cfg, err := source.DecodeCommonsConfig(params.Config)
	if err != nil {
		return source.Resolved{}, err
	}
	if cfg.Collection == source.CommonsLive {
		return p.resolveLive(ctx, cfg, params)
	}
	var ref reference
	if source.Decode(params.Ref, &ref) != nil {
		return source.Resolved{}, source.Failure("not_found", "invalid reviewed Commons reference")
	}
	index := slices.IndexFunc(p.files, func(file reviewedFile) bool {
		return file.PageID == ref.PageID && file.id() == params.ID
	})
	if index < 0 {
		return source.Resolved{}, source.Failure("not_found", "Commons file revision is outside the reviewed collection")
	}
	pin := p.files[index]
	items, err := p.fetch(ctx, p.files[index:index+1])
	if err != nil {
		return source.Resolved{}, err
	}
	candidate, ok := acceptedCandidate(pin, items[pin.PageID])
	if !ok {
		return source.Resolved{}, source.Failure("not_found", "Commons image or reuse terms differ from the reviewed revision")
	}
	return source.Resolved{Content: *candidate.Content, Attribution: candidate.Attribution, Dimensions: candidate.Dimensions}, nil
}

type imageInfo struct {
	ThumbnailURL string `json:"thumburl"`
	URL          string `json:"url"`
	PageURL      string `json:"descriptionurl"`
	MIME         string `json:"mime"`
	SHA1         string `json:"sha1"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	ExtMetadata  map[string]struct {
		Value string `json:"value"`
	} `json:"extmetadata"`
}

type filePage struct {
	Index     int         `json:"index"`
	PageID    int         `json:"pageid"`
	LastRevID int         `json:"lastrevid"`
	Title     string      `json:"title"`
	Namespace int         `json:"ns"`
	ImageInfo []imageInfo `json:"imageinfo"`
}

var termsFields = []string{"LicenseShortName", "LicenseUrl", "Artist", "Attribution", "Credit", "AttributionRequired", "Copyrighted", "Restrictions"}

func acceptedCandidate(pin reviewedFile, item filePage) (source.Candidate, bool) {
	if item.PageID != pin.PageID || item.Namespace != 6 || !strings.HasPrefix(item.Title, "File:") || len(item.ImageInfo) != 1 {
		return source.Candidate{}, false
	}
	info := item.ImageInfo[0]
	if info.SHA1 != pin.SHA1 || info.Width < 1 || info.Height < 1 ||
		(info.MIME != "image/jpeg" && info.MIME != "image/png") {
		return source.Candidate{}, false
	}
	terms := make([]string, 0, len(termsFields))
	for _, field := range termsFields {
		terms = append(terms, info.ExtMetadata[field].Value)
	}
	license, licenseURL := info.ExtMetadata["LicenseShortName"].Value, info.ExtMetadata["LicenseUrl"].Value
	artist, attribution := info.ExtMetadata["Artist"].Value, info.ExtMetadata["Attribution"].Value
	// The digest covers the author, requested credit, and reuse terms. Only
	// the reviewed plain-text credit is ever passed through to consumers.
	if strings.TrimSpace(pin.Credit) == "" || license == "" || licenseURL == "" ||
		info.ExtMetadata["Restrictions"].Value != "" ||
		(strings.TrimSpace(artist) == "" && strings.TrimSpace(attribution) == "") ||
		termsDigest(terms) != pin.TermsSHA256 {
		return source.Candidate{}, false
	}
	original := canonicalURL(info.URL, "upload.wikimedia.org", "/wikipedia/commons/", "original")
	page := canonicalURL(info.PageURL, "commons.wikimedia.org", "/wiki/File:", "")
	if original == "" || page == "" || pin.SHA256 == "" {
		return source.Candidate{}, false
	}
	ref, _ := json.Marshal(reference{item.PageID})
	c := source.Candidate{
		ID: pin.id(), MediaKind: "image", Ref: ref,
		Dimensions: &source.Dimensions{Width: info.Width, Height: info.Height},
		Content: &source.Content{
			Kind: "https", MediaKind: "image", MIMEType: info.MIME, URL: original, SHA256: pin.SHA256,
		},
		Attribution: &source.Attribution{
			Title:   strings.TrimSuffix(strings.TrimPrefix(item.Title, "File:"), extension(item.Title)),
			Author:  pin.Credit,
			PageURL: page, LicenseName: license, LicenseURL: strings.TrimRight(licenseURL, "/") + "/",
		},
	}
	return c, c.Validate() == nil
}

func extension(title string) string {
	i := strings.LastIndexByte(title, '.')
	if i < 0 {
		return ""
	}
	return title[i:]
}

// Length-prefix each raw field so even embedded delimiters in metadata cannot
// make two different sets of reuse terms produce the same input.
func termsDigest(terms []string) string {
	hash := sha256.New()
	for _, term := range terms {
		_, _ = io.WriteString(hash, strconv.Itoa(len(term))+":"+term)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func canonicalURL(raw, host, prefix, trackingContent string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != host || u.User != nil ||
		u.Fragment != "" || u.ForceQuery || !strings.HasPrefix(u.Path, prefix) || len(u.Path) == len(prefix) {
		return ""
	}
	if u.RawQuery != "" {
		query, err := url.ParseQuery(u.RawQuery)
		if trackingContent == "" || err != nil || len(query) != 3 ||
			!slices.Equal(query["utm_source"], []string{"commons.wikimedia.org"}) ||
			!slices.Equal(query["utm_campaign"], []string{"imageinfo"}) ||
			!slices.Equal(query["utm_content"], []string{trackingContent}) {
			return ""
		}
		u.RawQuery = ""
	}
	return u.String()
}

func (p *Provider) fetch(ctx context.Context, files []reviewedFile) (map[int]filePage, error) {
	items := make(map[int]filePage, len(files))
	if len(files) == 0 {
		return items, nil
	}
	ids := make([]string, len(files))
	wanted := make(map[int]bool, len(files))
	for i, file := range files {
		ids[i] = strconv.Itoa(file.PageID)
		wanted[file.PageID] = true
	}
	body, err := p.imageAPI(ctx, url.Values{"pageids": {strings.Join(ids, "|")}})
	if err != nil {
		return nil, err
	}
	if body.Query == nil || body.Query.Pages == nil {
		return nil, source.Failure("unavailable", "Commons API returned incomplete metadata")
	}
	for _, item := range body.Query.Pages {
		if !wanted[item.PageID] {
			continue
		}
		if _, duplicate := items[item.PageID]; duplicate {
			return nil, errors.New("Commons API returned duplicate file metadata")
		}
		items[item.PageID] = item
	}
	return items, nil
}

type apiResponse struct {
	Error *struct{ Code string } `json:"error"`
	Query *struct {
		Pages      []filePage
		Categories []struct {
			Name   string `json:"category"`
			Hidden bool   `json:"hidden"`
		} `json:"allcategories"`
	} `json:"query"`
	Warning  json.RawMessage `json:"warnings"`
	Continue struct {
		SearchOffset int `json:"gsroffset"`
	} `json:"continue"`
}

func (p *Provider) imageAPI(ctx context.Context, query url.Values) (apiResponse, error) {
	query.Set("prop", "imageinfo|info")
	query.Set("iiprop", "url|size|mime|sha1|extmetadata")
	query.Set("iiextmetadatafilter", strings.Join(termsFields, "|")+"|License")
	query.Set("iiextmetadatalanguage", "en")
	return p.api(ctx, query)
}

func (p *Provider) api(ctx context.Context, query url.Values) (apiResponse, error) {
	var body apiResponse
	query.Set("action", "query")
	query.Set("format", "json")
	query.Set("formatversion", "2")
	query.Set("maxlag", "5")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return body, source.Failure("unavailable", "could not make a Commons metadata request")
	}
	req.Header.Set("User-Agent", source.UserAgent)
	resp, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return body, ctx.Err()
		}
		return body, source.Failure("unavailable", "could not reach Wikimedia Commons")
	}
	defer resp.Body.Close()
	retryAfter := source.RetryAfter(resp.Header.Get("Retry-After"), time.Now())
	if resp.StatusCode != http.StatusOK {
		code := "unavailable"
		if resp.StatusCode == http.StatusTooManyRequests {
			code = "rate_limited"
		}
		return body, &source.Error{Code: code, Message: "Commons metadata request failed", RetryAfterSeconds: retryAfter}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, source.MaxResponseBytes+1))
	if err != nil || len(raw) > source.MaxResponseBytes {
		return body, source.Failure("unavailable", "Commons metadata response exceeds limits or could not be read")
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return body, source.Failure("unavailable", "Commons returned invalid metadata")
	}
	if body.Error != nil {
		code := "unavailable"
		if body.Error.Code == "maxlag" || body.Error.Code == "ratelimited" {
			code = "rate_limited"
		}
		return body, &source.Error{Code: code, Message: "Commons API could not provide metadata", RetryAfterSeconds: retryAfter}
	}
	if len(body.Warning) != 0 {
		return body, source.Failure("unavailable", "Commons API returned incomplete metadata")
	}
	return body, nil
}
