package local

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vadim82/panora/internal/source"
)

func settings(t *testing.T, root string, depth int) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(source.LocalConfig{Root: root, MaxDepth: depth})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestLocalPaginationDepthAndResolve(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 205; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("image-%03d.PNG", i)), []byte("headers are the host's concern"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "extra.jpg"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	provider := Provider{}
	params := source.DiscoverParams{Config: settings(t, root, 0), Cursor: json.RawMessage("null"), Limit: 100}
	ids := map[string]bool{}
	var first source.Candidate
	for pageIndex, want := range []int{100, 100, 5} {
		page, err := provider.Discover(context.Background(), params)
		if err != nil || len(page.Candidates) != want {
			t.Fatalf("page %d: %+v %v", pageIndex, page, err)
		}
		if pageIndex == 0 {
			first = page.Candidates[0]
		}
		for _, candidate := range page.Candidates {
			if ids[candidate.ID] || candidate.Content.Kind != "local" || !filepath.IsAbs(candidate.Content.Path) {
				t.Fatalf("bad candidate: %+v", candidate)
			}
			ids[candidate.ID] = true
		}
		params.Cursor = page.NextCursor
	}
	if string(params.Cursor) != "null" || len(ids) != 205 {
		t.Fatal("scan did not finish cleanly")
	}
	resolved, err := provider.Resolve(context.Background(), source.ResolveParams{Config: params.Config, ID: first.ID, Ref: first.Ref})
	if err != nil || resolved.Content.Path != first.Content.Path {
		t.Fatalf("resolve: %+v %v", resolved, err)
	}
	page, err := provider.Discover(context.Background(), source.DiscoverParams{Config: settings(t, root, 1), Limit: 100})
	if err != nil || len(page.Candidates) != 100 {
		t.Fatal(err)
	}
	_, err = provider.Discover(context.Background(), source.DiscoverParams{Config: params.Config, Cursor: page.NextCursor, Limit: 100})
	var typed *source.Error
	if !errors.As(err, &typed) || typed.Code != "invalid_cursor" {
		t.Fatalf("cross-config cursor accepted: %v", err)
	}
}

func TestLocalRejectsUnsafeReferencesAndSkipsLinks(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outside, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.png")); err != nil {
		t.Fatal(err)
	}
	config := settings(t, root, 0)
	provider := Provider{}
	page, err := provider.Discover(context.Background(), source.DiscoverParams{Config: config, Limit: 100})
	if err != nil || len(page.Candidates) != 0 || string(page.NextCursor) != "null" {
		t.Fatalf("link discovered: %+v %v", page, err)
	}
	ref, _ := json.Marshal(map[string]string{"path": filepath.Join(root, "link.png")})
	if _, err := provider.Resolve(context.Background(), source.ResolveParams{Config: config, ID: "link.png", Ref: ref}); err == nil {
		t.Fatal("resolved escaping link")
	}
	ref, _ = json.Marshal(map[string]string{"path": outside})
	if _, err := provider.Resolve(context.Background(), source.ResolveParams{Config: config, ID: "../outside.png", Ref: ref}); err == nil {
		t.Fatal("resolved outside path")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.Discover(ctx, source.DiscoverParams{Config: config, Limit: 100}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestLocalWireHandshakeAndTypedErrors(t *testing.T) {
	for _, test := range []struct {
		request string
		code    int
	}{
		{`{"jsonrpc":"2.0","id":"a","method":"describe","params":{"protocol":2}}`, 0},
		{`{"jsonrpc":"2.0","id":"a","method":"describe","params":{"protocol":1}}`, 1000},
		{`{"jsonrpc":"2.0","id":"a","method":"other","params":{"protocol":2}}`, -32601},
		{`{"jsonrpc":"2.0","id":"a","method":"validate","params":{"protocol":2,"config":{"root":"relative","max_depth":0}}}`, 1000},
	} {
		var output bytes.Buffer
		if err := source.Serve(context.Background(), Provider{}, bytes.NewBufferString(test.request+"\n"), &output); err != nil {
			t.Fatal(err)
		}
		var response source.Response
		if err := json.Unmarshal(output.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.JSONRPC != source.JSONRPC || string(response.ID) != `"a"` {
			t.Fatalf("envelope: %+v", response)
		}
		if test.code != 0 && (response.Error == nil || response.Error.Code != test.code) {
			t.Fatalf("typed error: %+v", response)
		}
		if test.code == 0 {
			var description source.Description
			if err := json.Unmarshal(response.Result, &description); err != nil || description.ID != "local" || description.Protocol != source.Protocol {
				t.Fatalf("describe: %+v %v", description, err)
			}
		}
	}
}

func TestDiscoveryRevisionInvalidatesCursor(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.png", "b.png"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	config := source.LocalConfig{Root: root, MaxDepth: 0, Revision: "first"}
	raw, _ := json.Marshal(config)
	page, err := (Provider{}).Discover(context.Background(), source.DiscoverParams{Config: raw, Limit: 1})
	if err != nil || source.IsNull(page.NextCursor) {
		t.Fatalf("first page: %+v %v", page, err)
	}
	var oldCursor struct {
		Config string `json:"config"`
		Offset int    `json:"offset"`
	}
	if err := json.Unmarshal(page.NextCursor, &oldCursor); err != nil {
		t.Fatal(err)
	}
	unanchored, err := json.Marshal(oldCursor)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (Provider{}).Discover(t.Context(), source.DiscoverParams{Config: raw, Cursor: unanchored, Limit: 1})
	var typed *source.Error
	if !errors.As(err, &typed) || typed.Code != "invalid_cursor" {
		t.Fatalf("unanchored cursor accepted: %v", err)
	}
	config.Revision = "second"
	raw, _ = json.Marshal(config)
	_, err = (Provider{}).Discover(context.Background(), source.DiscoverParams{Config: raw, Cursor: page.NextCursor, Limit: 1})
	if !errors.As(err, &typed) || typed.Code != "invalid_cursor" {
		t.Fatalf("cursor reused under changed revision: %v", err)
	}
}
