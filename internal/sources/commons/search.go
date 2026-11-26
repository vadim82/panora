package commons

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/vadim82/panora/internal/source"
	"golang.org/x/net/html"
)

const searchPageSize = 8

func (p *Provider) Search(ctx context.Context, filter source.CommonsSearch) (source.CommonsSearchPage, error) {
	page := source.CommonsSearchPage{Version: 1, Results: []source.CommonsSearchResult{}}
	if err := filter.Validate(); err != nil {
		return page, err
	}
	select {
	case p.searchSlot <- struct{}{}:
		defer func() { <-p.searchSlot }()
	default:
		return page, source.Failure("busy", "a Commons search is already running")
	}
	if time.Now().Before(p.searchRetryUntil) {
		return page, source.Failure("rate_limited", "Commons requested a pause before searching again")
	}
	ctx, cancel := context.WithTimeout(ctx, 7*time.Second)
	defer cancel()
	body, err := p.imageAPI(ctx, url.Values{
		"generator": {"search"}, "gsrsearch": {searchTerms(filter)}, "gsrnamespace": {"6"},
		"gsrlimit": {strconv.Itoa(searchPageSize)}, "gsroffset": {strconv.Itoa(filter.Offset)}, "iiurlwidth": {"250"},
	})
	if err != nil {
		var failure *source.Error
		if errors.As(err, &failure) {
			p.searchRetryUntil = retryUntil(failure)
		}
		return page, err
	}
	if next := body.Continue.SearchOffset; next > filter.Offset && next <= source.CommonsMaxOffset {
		page.NextOffset = next
	}
	if body.Query == nil {
		return page, nil
	}
	if len(body.Query.Pages) > searchPageSize {
		return page, source.Failure("unavailable", "Commons search exceeded the page limit")
	}
	items := body.Query.Pages
	slices.SortStableFunc(items, func(a, b filePage) int { return cmp.Compare(a.Index, b.Index) })
	seen := make(map[int]bool, len(items))
	var previews []string
	for _, item := range items {
		c, approval, ok := liveCandidate(item)
		if !ok || seen[item.PageID] || !filter.Matches(c.Dimensions.Width, c.Dimensions.Height, c.Content.MIMEType) {
			continue
		}
		seen[item.PageID] = true
		result := source.CommonsSearchResult{
			CommonsApproval: approval,
			Title:           c.Attribution.Title, Width: c.Dimensions.Width, Height: c.Dimensions.Height,
			Author: c.Attribution.Author, LicenseName: c.Attribution.LicenseName,
			LicenseURL: c.Attribution.LicenseURL, PageURL: c.Attribution.PageURL,
		}
		page.Results = append(page.Results, result)
		previews = append(previews, item.ImageInfo[0].ThumbnailURL)
	}
	p.resolveThumbnails(ctx, page.Results, previews)
	return page, nil
}

func searchTerms(filter source.CommonsSearch) string {
	terms := []string{strings.TrimSpace(filter.Query)}
	for _, category := range filter.CategoryNames() {
		terms = append(terms, fmt.Sprintf("incategory:%q", strings.TrimSpace(category)))
	}
	if filter.Format != "" {
		terms = append(terms, `filemime:"image/`+filter.Format+`"`)
	} else {
		// The search backend's bitmap class is broader than our two supported
		// MIME types. Verify individual results before they leave the provider.
		terms = append(terms, "filetype:bitmap")
	}
	if filter.ExactWidth > 0 {
		terms = append(terms, fmt.Sprintf("filew:%d fileh:%d", filter.ExactWidth, filter.ExactHeight))
	} else {
		if filter.MinWidth > 0 {
			terms = append(terms, fmt.Sprintf("filew:>%d", filter.MinWidth))
		}
		if filter.MinHeight > 0 {
			terms = append(terms, fmt.Sprintf("fileh:>%d", filter.MinHeight))
		}
	}
	return strings.TrimSpace(strings.Join(terms, " "))
}

func (p *Provider) resolveLive(ctx context.Context, cfg source.CommonsConfig, params source.ResolveParams) (source.Resolved, error) {
	var approval source.CommonsApproval
	if source.Decode(params.Ref, &approval) != nil || !approval.Valid() || !slices.Contains(cfg.Approved, approval) {
		return source.Resolved{}, source.Failure("not_found", "Commons file has no current approval")
	}
	items, err := p.fetch(ctx, []reviewedFile{{PageID: approval.PageID}})
	if err != nil {
		return source.Resolved{}, err
	}
	candidate, actual, ok := liveCandidate(items[approval.PageID])
	if !ok || actual != approval || candidate.ID != params.ID || !cfg.Allows(candidate) {
		return source.Resolved{}, source.Failure("not_found", "Commons file revision or reuse terms changed")
	}
	return source.Resolved{Content: *candidate.Content, Attribution: candidate.Attribution, Dimensions: candidate.Dimensions}, nil
}

var reuseLicense = regexp.MustCompile(`^https?://creativecommons\.org/((?:licenses/(?:by|by-sa)/(?:1\.0|2\.0|2\.5|3\.0|4\.0)(?:/[a-z-]+)?|publicdomain/(?:zero|mark)/1\.0))(?:/(?:deed(?:\.[a-zA-Z_-]+)?)?)?$`)

func liveLicense(info imageInfo, pageURL string) (string, string) {
	metadata := info.ExtMetadata
	name := safeText(metadata["LicenseShortName"].Value, 128)
	raw := strings.TrimSpace(metadata["LicenseUrl"].Value)
	match := reuseLicense.FindStringSubmatch(raw)
	if len(match) != 0 {
		return name, "https://creativecommons.org/" + match[1] + "/"
	}
	// Commons public-domain templates describe the reason on the file page.
	if name == "Public domain" && raw == "" && metadata["License"].Value == "pd" &&
		strings.EqualFold(metadata["Copyrighted"].Value, "false") &&
		strings.EqualFold(metadata["AttributionRequired"].Value, "false") {
		return name, pageURL
	}
	return "", ""
}

func liveCandidate(item filePage) (source.Candidate, source.CommonsApproval, bool) {
	var c source.Candidate
	var approval source.CommonsApproval
	if item.PageID < 1 || item.Namespace != 6 || !strings.HasPrefix(item.Title, "File:") || len(item.ImageInfo) != 1 {
		return c, approval, false
	}
	info := item.ImageInfo[0]
	const maxPixels = 64_000_000
	if info.Width < 1 || info.Height < 1 || info.Width > maxPixels/info.Height ||
		(info.MIME != "image/jpeg" && info.MIME != "image/png") {
		return c, approval, false
	}
	if info.ExtMetadata["Restrictions"].Value != "" {
		return c, approval, false
	}
	credit := info.ExtMetadata["Attribution"].Value
	if strings.TrimSpace(credit) == "" {
		credit = info.ExtMetadata["Artist"].Value
	}
	author := safeText(credit, 512)
	title := safeText(strings.TrimSuffix(strings.TrimPrefix(item.Title, "File:"), extension(item.Title)), 512)
	original := canonicalURL(info.URL, "upload.wikimedia.org", "/wikipedia/commons/", "original")
	page := canonicalURL(info.PageURL, "commons.wikimedia.org", "/wiki/File:", "")
	license, licenseURL := liveLicense(info, page)
	if author == "" || title == "" || original == "" || page == "" || license == "" || licenseURL == "" {
		return c, approval, false
	}
	terms := make([]string, 0, len(termsFields)+2)
	for _, field := range termsFields {
		terms = append(terms, info.ExtMetadata[field].Value)
	}
	terms = append(terms, info.ExtMetadata["License"].Value)
	if licenseURL == page {
		if item.LastRevID < 1 {
			return c, approval, false
		}
		terms = append(terms, strconv.Itoa(item.LastRevID))
	}
	approval = source.CommonsApproval{PageID: item.PageID, Revision: info.SHA1, TermsDigest: termsDigest(terms)}
	if !approval.Valid() {
		return c, approval, false
	}
	ref, _ := json.Marshal(approval)
	c = source.Candidate{
		ID: strconv.Itoa(item.PageID) + "-" + info.SHA1, MediaKind: "image", Ref: ref,
		Dimensions: &source.Dimensions{Width: info.Width, Height: info.Height},
		Content:    &source.Content{Kind: "https", MediaKind: "image", MIMEType: info.MIME, URL: original, RevisionSHA1: info.SHA1},
		Attribution: &source.Attribution{
			Title: title, Author: author, PageURL: page, LicenseName: license,
			LicenseURL: licenseURL,
		},
	}
	return c, approval, c.Validate() == nil
}

func safeText(value string, max int) string {
	if len(value) > 16<<10 {
		return ""
	}
	tokenizer := html.NewTokenizer(strings.NewReader(value))
	var text strings.Builder
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			plain := strings.Join(strings.Fields(text.String()), " ")
			if len(plain) > max || strings.ContainsFunc(plain, unicode.IsControl) {
				return ""
			}
			return plain
		case html.TextToken:
			_, _ = text.Write(tokenizer.Text())
		case html.StartTagToken, html.SelfClosingTagToken:
			if name, _ := tokenizer.TagName(); string(name) == "br" || string(name) == "p" {
				_ = text.WriteByte(' ')
			} else if string(name) == "script" || string(name) == "style" {
				return ""
			}
		}
	}
}
