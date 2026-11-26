package commons

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vadim82/panora/internal/source"
)

var featured = json.RawMessage(`{"collection":"featured-landscapes"}`)

func reviewedFixture(t *testing.T, ids ...int) []reviewedFile {
	t.Helper()
	files := make([]reviewedFile, 0, len(ids))
	for _, id := range ids {
		files = append(files, reviewedFile{
			PageID: id, SHA1: strings.Repeat("a", 40), SHA256: strings.Repeat("b", 64),
			TermsSHA256: termsDigest([]string{"CC BY-SA 4.0", "https://creativecommons.org/licenses/by-sa/4.0", "<a>Photographer</a>", "Requested credit", "Own work", "true", "True", ""}),
			Credit:      "Requested credit",
		})
	}
	return files
}

func providerFixture(t *testing.T, handler http.HandlerFunc, ids ...int) *Provider {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	pins, err := json.Marshal(reviewedFixture(t, ids...))
	if err != nil {
		t.Fatal(err)
	}
	return newProvider(server.URL, pins)
}

func fileFixture(id string) string {
	return fmt.Sprintf(`{"pageid":%s,"ns":6,"title":"File:Mountain.jpg","imageinfo":[{
		"url":"https://upload.wikimedia.org/wikipedia/commons/a/ab/Mountain.jpg?utm_source=commons.wikimedia.org&utm_campaign=imageinfo&utm_content=original",
		"descriptionurl":"https://commons.wikimedia.org/wiki/File:Mountain.jpg",
		"width":5120,"height":2880,"mime":"image/jpeg","sha1":"%s",
		"extmetadata":{"Artist":{"value":"<a>Photographer</a>"},"Attribution":{"value":"Requested credit"},"Credit":{"value":"Own work"},
		"LicenseShortName":{"value":"CC BY-SA 4.0"},"LicenseUrl":{"value":"https://creativecommons.org/licenses/by-sa/4.0"},
		"AttributionRequired":{"value":"true"},"Copyrighted":{"value":"True"}}}]}`, id, strings.Repeat("a", 40))
}

func TestDiscoveryPaginatesReviewedIDsAndResolveRechecks(t *testing.T) {
	calls := make(chan string, 4)
	p := providerFixture(t, func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("pageids")
		calls <- id
		if r.Header.Get("User-Agent") != source.UserAgent || r.URL.Query().Get("maxlag") != "5" {
			t.Error("missing Commons request policy")
		}
		fmt.Fprintf(w, `{"query":{"pages":[%s,%s]}}`, fileFixture("9"), fileFixture(id))
	}, 1, 2)
	ctx := context.Background()
	if err := p.Validate(ctx, featured); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Discover(ctx, source.DiscoverParams{Config: json.RawMessage(`{"collection":"arbitrary"}`), Limit: 1}); err == nil || len(calls) != 0 {
		t.Fatal("validation contacted Commons or accepted arbitrary collection")
	}
	first, err := p.Discover(ctx, source.DiscoverParams{Config: featured, Limit: 1})
	if err != nil || len(first.Candidates) != 1 || source.IsNull(first.NextCursor) {
		t.Fatalf("first page %+v, %v", first, err)
	}
	c := first.Candidates[0]
	if c.Attribution.Author != "Requested credit" || c.Attribution.LicenseName != "CC BY-SA 4.0" ||
		c.Content.URL != "https://upload.wikimedia.org/wikipedia/commons/a/ab/Mountain.jpg" ||
		c.Content.SHA256 != strings.Repeat("b", 64) {
		t.Fatalf("wrong reviewed metadata: %+v, %+v", c.Attribution, c.Content)
	}
	second, err := p.Discover(ctx, source.DiscoverParams{Config: featured, Cursor: first.NextCursor, Limit: 1})
	if err != nil || len(second.Candidates) != 1 || !source.IsNull(second.NextCursor) || second.Candidates[0].ID == c.ID {
		t.Fatalf("second page %+v, %v", second, err)
	}
	again, err := p.Discover(ctx, source.DiscoverParams{Config: featured, Limit: 1})
	if err != nil || len(again.Candidates) != 1 || again.Candidates[0].ID != c.ID {
		t.Fatalf("identity changed across refreshes: %+v, %v", again, err)
	}
	resolve, err := p.Resolve(ctx, source.ResolveParams{Config: featured, ID: c.ID, Ref: c.Ref})
	if err != nil || resolve.Content != *c.Content || len(calls) != 4 {
		t.Fatalf("resolve %+v, %v", resolve, err)
	}
	if _, err := p.Resolve(ctx, source.ResolveParams{Config: featured, ID: c.ID, Ref: json.RawMessage(`{"page_id":9}`)}); err == nil {
		t.Fatal("resolved a page outside review")
	}
}

func TestRejectsChangedOrUnsupportedImages(t *testing.T) {
	pin := reviewedFixture(t, 1)[0]
	for _, change := range []struct{ old, new string }{
		{strings.Repeat("a", 40), strings.Repeat("c", 40)},
		{"image/jpeg", "image/webp"},
		{"Requested credit", ""},
		{"Photographer", "<script>tampered</script>"},
		{"CC BY-SA 4.0", ""},
		{"utm_content=original", "access_token=bogus"},
		{"upload.wikimedia.org", "example.org"},
		{"\"pageid\":1", "\"pageid\":9"},
	} {
		var page filePage
		if err := json.Unmarshal([]byte(strings.Replace(fileFixture("1"), change.old, change.new, 1)), &page); err != nil {
			t.Fatal(err)
		}
		if _, ok := acceptedCandidate(pin, page); ok {
			t.Errorf("accepted changed Commons field %q", change.old)
		}
	}
}

func TestCommonsThrottling(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusOK} {
		p := providerFixture(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "23")
			w.WriteHeader(status)
			fmt.Fprint(w, `{"error":{"code":"maxlag"}}`)
		}, 1)
		_, err := p.Discover(context.Background(), source.DiscoverParams{Config: featured, Limit: 1})
		var typed *source.Error
		if !errors.As(err, &typed) || typed.Code != "rate_limited" || typed.RetryAfterSeconds == nil || *typed.RetryAfterSeconds != 23 {
			t.Fatalf("incorrect retry hint: %v", err)
		}
	}
}

func TestBundledCollectionPinsAreUniqueAndWellFormed(t *testing.T) {
	p := New()
	seen := make(map[int]bool)
	if len(p.files) < 2 || len(p.files) > maxBatch {
		t.Fatalf("starter collection should fit one bounded metadata request: %d", len(p.files))
	}
	for _, file := range p.files {
		if seen[file.PageID] || file.PageID < 1 || len(file.SHA1) != 40 ||
			len(file.SHA256) != 64 || len(file.TermsSHA256) != 64 || strings.TrimSpace(file.Credit) == "" {
			t.Fatalf("invalid reviewed Commons pin: %+v", file)
		}
		seen[file.PageID] = true
	}
}

func TestSearchApprovesOnlyMatchingUnchangedFiles(t *testing.T) {
	filter := source.CommonsSearch{Query: "night sky", Category: "Landscape photographs", Format: "jpeg",
		MinWidth: 6000, MinHeight: 6000, ExactWidth: 5120, ExactHeight: 2880, Offset: 8}
	metadata := make(chan string, 1)
	requested := make(chan string, 4)
	p := providerFixture(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("generator") != "" {
			requested <- "search"
			if q.Get("gsrsearch") != `night sky incategory:"Landscape photographs" filemime:"image/jpeg" filew:5120 fileh:2880` ||
				q.Get("gsrnamespace") != "6" || q.Get("gsrlimit") != "8" || q.Get("gsroffset") != "8" {
				t.Errorf("incorrect search filters: %v", q)
			}
			second := strings.Replace(fileFixture("2"), `{"pageid"`, `{"index":2,"pageid"`, 1)
			first := strings.Replace(fileFixture("1"), `{"pageid"`, `{"index":1,"pageid"`, 1)
			size := strings.Replace(fileFixture("3"), "5120", "5119", 1)
			badCredit := strings.Replace(fileFixture("4"), "Requested credit", "<script>bad</script>", 1)
			restricted := strings.Replace(fileFixture("5"), `"extmetadata":{`, `"extmetadata":{"Restrictions":{"value":"trademarked"},`, 1)
			fmt.Fprintf(w, `{"continue":{"gsroffset":16},"query":{"pages":[%s,%s,%s,%s,%s,%s]}}`, second, first, first, size, badCredit, restricted)
			return
		}
		requested <- q.Get("pageids")
		file := fileFixture("1")
		select {
		case file = <-metadata:
		default:
		}
		fmt.Fprintf(w, `{"query":{"pages":[%s,%s]}}`, file, fileFixture("2"))
	})
	ctx := t.Context()
	search, err := p.Search(ctx, filter)
	if err != nil || len(search.Results) != 2 || search.NextOffset != 16 || <-requested != "search" {
		t.Fatalf("unexpected search page: %+v, %v", search, err)
	}
	first := search.Results[0]
	if first.PageID != 1 || first.Width != 5120 || first.Height != 2880 || first.Author != "Requested credit" || !first.CommonsApproval.Valid() {
		t.Fatalf("wrong review data: %+v", first)
	}
	filter.Offset = 0
	settings := source.CommonsConfig{CommonsSearch: filter, Collection: source.CommonsLive}
	config, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Discover(ctx, source.DiscoverParams{Config: config, Limit: 8}); err == nil || len(requested) != 0 {
		t.Fatal("unapproved search became eligible or triggered discovery")
	}
	settings.Approved = []source.CommonsApproval{first.CommonsApproval}
	config, err = json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	page, err := p.Discover(ctx, source.DiscoverParams{Config: config, Limit: 8})
	if err != nil || len(page.Candidates) != 1 || <-requested != "1" {
		t.Fatalf("unexpected discovery result: %+v, %v", page, err)
	}
	candidate := page.Candidates[0]
	params := source.ResolveParams{Config: config, ID: candidate.ID, Ref: candidate.Ref}
	resolved, err := p.Resolve(ctx, params)
	if err != nil || resolved.Content.RevisionSHA1 != first.Revision || <-requested != "1" {
		t.Fatalf("unexpected resolved image: %+v, %v", resolved, err)
	}
	metadata <- strings.Replace(fileFixture("1"), "Requested credit", "Updated credit", 1)
	if _, err := p.Resolve(ctx, params); err == nil || <-requested != "1" {
		t.Fatal("an approval authorized changed Commons terms")
	}
	metadata <- strings.Replace(fileFixture("1"), first.Revision, strings.Repeat("b", 40), 1)
	page, err = p.Discover(ctx, source.DiscoverParams{Config: config, Limit: 8})
	if err != nil || len(page.Candidates) != 0 || <-requested != "1" {
		t.Fatalf("changed revisions remained eligible: %+v, %v", page, err)
	}
}

func TestSearchPublicDomainMetadata(t *testing.T) {
	raw := strings.NewReplacer("CC BY-SA 4.0", "Public domain",
		"https://creativecommons.org/licenses/by-sa/4.0", "",
		`"AttributionRequired":{"value":"true"}`, `"AttributionRequired":{"value":"false"}`,
		`"Copyrighted":{"value":"True"}`, `"Copyrighted":{"value":"False"},"License":{"value":"pd"}`).Replace(fileFixture("1"))
	var item filePage
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		t.Fatal(err)
	}
	candidate, _, ok := liveCandidate(item)
	if !ok || candidate.Attribution.LicenseName != "Public domain" || candidate.Attribution.LicenseURL != candidate.Attribution.PageURL {
		t.Fatalf("missing Commons public domain details: %+v", candidate.Attribution)
	}
	delete(item.ImageInfo[0].ExtMetadata, "License")
	if _, _, ok := liveCandidate(item); ok {
		t.Fatal("public domain label without corroborating metadata was accepted")
	}
}

func TestSearchHonorsRetryBeforeContactingCommonsAgain(t *testing.T) {
	requested := make(chan struct{}, 2)
	p := providerFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		requested <- struct{}{}
		w.Header().Set("Retry-After", "12")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	for range 2 {
		_, err := p.Search(t.Context(), source.CommonsSearch{Category: "Night photography"})
		var failure *source.Error
		if !errors.As(err, &failure) || failure.Code != "rate_limited" {
			t.Fatalf("expected throttled search: %v", err)
		}
	}
	if len(requested) != 1 {
		t.Fatalf("expected one upstream search, got %d", len(requested))
	}
}
