package cli

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// These rules enforce the documented dependency direction, rather than naming
// individual files or prescribing how each package implements its responsibility.
func TestInternalPackageBoundaries(t *testing.T) {
	allowed := map[string]map[string]bool{
		"appservice":        {"control": true, "filelock": true},
		"cli":               {"appservice": true, "control": true, "desktop": true, "playback": true, "service": true, "source": true, "sources/local": true, "sources/wallhaven": true, "sources/commons": true},
		"service":           {"control": true, "playback": true},
		"playback":          {"desktop": true, "filelock": true, "source": true, "localfiles": true, "catalog": true, "cache": true, "omarchytheme": true},
		"catalog":           {"source": true},
		"cache":             {"source": true, "filelock": true, "localfiles": true},
		"control":           {"playback": true, "filelock": true, "source": true},
		"desktop":           {"filelock": true},
		"filelock":          {},
		"source":            {"filelock": true, "localfiles": true},
		"omarchytheme":      {"localfiles": true},
		"localfiles":        {},
		"sources/local":     {"source": true, "localfiles": true},
		"sources/wallhaven": {"source": true},
		"sources/commons":   {"source": true},
	}
	for name, dependencies := range allowed {
		dir := filepath.Join("..", "..", "internal", name)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, entry.Name()), nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imported := range file.Imports {
				path, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				if (name != "cli" && strings.HasPrefix(path, "github.com/spf13/")) || strings.HasPrefix(path, "github.com/vadim82/panora/cmd/") {
					t.Errorf("%s/%s depends on CLI package %s", name, entry.Name(), path)
				}
				if dependency, internal := strings.CutPrefix(path, "github.com/vadim82/panora/internal/"); internal && !dependencies[dependency] {
					t.Errorf("%s/%s has undocumented dependency %s", name, entry.Name(), path)
				}
				if name == "playback" && (path == "net/http" || path == "net") {
					t.Errorf("playback must not implement a control transport: %s", entry.Name())
				}
			}
		}
	}
}
