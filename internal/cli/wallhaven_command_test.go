package cli

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/vadim82/panora/internal/source"
)

func TestBundledWallhavenJSONRPCHandshake(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	provider := &source.Client{Manifest: source.Manifest{ID: "wallhaven", Protocol: source.Protocol, Command: []string{binary, "provider", "wallhaven"}}}
	description, err := provider.Describe(context.Background())
	if err != nil || description.ID != "wallhaven" || description.ContentKinds[0] != "https" {
		t.Fatalf("describe through child: %+v %v", description, err)
	}
	if err := provider.Validate(context.Background(), json.RawMessage(`{"query":"nature"}`)); err != nil {
		t.Fatal(err)
	}
	if err := provider.Validate(context.Background(), json.RawMessage(`{"query":"nature","purity":"111"}`)); err == nil {
		t.Fatal("unsupported SFW override accepted")
	}
}
