package source

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPythonRemoteMetadataContract(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("../../testdata/plugins/python-remote/source.py")
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{Manifest: Manifest{ID: "remote", Protocol: Protocol, Command: []string{python, script}}}
	ctx := context.Background()
	description, err := client.Describe(ctx)
	if err != nil || description.ContentKinds[0] != "https" || len(description.FilterHints) != 3 {
		t.Fatalf("describe: %+v %v", description, err)
	}
	if err := client.Validate(ctx, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	filter := &FilterHints{MinWidth: 5120, MinHeight: 1440, Ratio: &Dimensions{Width: 32, Height: 9}}
	page, err := client.Discover(ctx, DiscoverParams{Config: json.RawMessage(`{}`), Limit: 1, Filter: filter})
	if err != nil || len(page.Candidates) != 1 {
		t.Fatalf("discover: %+v %v", page, err)
	}
	c := page.Candidates[0]
	if c.Dimensions != nil || c.ID != "mountain-123" || c.Content.URL != "https://images.example.org/mountain-123.png" || c.Attribution.Author != "Alex" {
		t.Fatalf("metadata lost: %+v", c)
	}
	var ref struct {
		Filter FilterHints `json:"filter"`
	}
	if json.Unmarshal(c.Ref, &ref) != nil || ref.Filter.MinWidth != 5120 || ref.Filter.Ratio.Width != 32 {
		t.Fatalf("filter hints lost: %s", c.Ref)
	}
	result, err := client.Resolve(ctx, ResolveParams{Config: json.RawMessage(`{}`), ID: c.ID, Ref: c.Ref})
	if err != nil || result.Dimensions == nil || result.Dimensions.Width != 5120 || result.Attribution.LicenseURL == "" {
		t.Fatalf("resolve: %+v %v", result, err)
	}
	_, err = client.Discover(ctx, DiscoverParams{Config: json.RawMessage(`{"rate_limited":true}`), Limit: 1})
	var failure *Error
	if !errors.As(err, &failure) || failure.Code != "rate_limited" || failure.RetryAfterSeconds == nil || *failure.RetryAfterSeconds != 120 {
		t.Fatalf("retry lost: %v", err)
	}
}

func TestClientRejectsUnsupportedRemoteMetadata(t *testing.T) {
	for _, tc := range []struct{ name, change string }{
		{"video", "c['media_kind']='video'"},
		{"scene", "c['content']['kind']='scene'"},
		{"unknown MIME", "c['content']['mime_type']='image/gif'"},
		{"HTTP", "c['content']['url']='http://example.org/image.png'"},
		{"userinfo", "c['content']['url']='https://user:secret@example.org/image.png'"},
		{"signed query", "c['content']['url']='https://example.org/image.png?token=secret'"},
		{"fragment", "c['content']['url']='https://example.org/image.png#secret'"},
		{"two locations", "c['content']['path']='/tmp/image.png'"},
		{"unknown dimensions", "c['dimensions']={'width':0,'height':1440}"},
		{"incompatible requirement", "c['requires']=['animated-video']"},
		{"unsafe attribution", "c['attribution']={'page_url':'javascript:secret'}"},
		{"missing cursor", "result.pop('next_cursor')"},
		{"oversized cursor", "result['next_cursor']='x'*4097"},
		{"duplicate ID", "result['candidates'].append(c)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := pythonClient(t, `import json,sys
r=json.load(sys.stdin)
c={'id':'a','media_kind':'image','ref':{'key':'a'},'content':{'media_kind':'image','kind':'https','url':'https://example.org/image.png'}}
result={'candidates':[c],'next_cursor':None}
`+tc.change+`
print(json.dumps({'jsonrpc':'2.0','id':r['id'],'result':result}))
`)
			if _, err := client.Discover(context.Background(), DiscoverParams{Config: json.RawMessage(`{}`), Limit: 2}); err == nil {
				t.Fatal("unsupported metadata accepted")
			}
		})
	}
}
