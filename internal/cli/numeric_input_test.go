package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/vadim82/panora/internal/desktop"
)

func TestInvalidNumericConfigurationDoesNotReachBackend(t *testing.T) {
	maxInt := strconv.Itoa(int(^uint(0) >> 1))
	for _, filter := range []string{"ratio = '" + maxInt + "0:1'", "min_width = 0", "min_height = " + maxInt + "0"} {
		t.Run(filter, func(t *testing.T) {
			root := t.TempDir()
			path := writeNextConfig(t, root)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			// Change one existing field rather than introducing duplicate TOML keys.
			key := strings.SplitN(filter, " = ", 2)[0]
			lines := strings.Split(string(data), "\n")
			for i, line := range lines {
				if strings.HasPrefix(line, key+" = ") {
					lines[i] = filter
				}
			}
			if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0600); err != nil {
				t.Fatal(err)
			}
			backend := &fakeWallpaperBackend{}
			calls := 0
			command := newServeCommand(func(context.Context) (desktop.WallpaperBackend, error) { calls++; return backend, nil })
			command.SetOut(io.Discard)
			command.SetErr(io.Discard)
			command.SetArgs([]string{"--config", path, "--state-file", filepath.Join(t.TempDir(), "state.db"), "--socket", shortSocket(t)})
			if err := command.ExecuteContext(context.Background()); err == nil {
				t.Fatal("invalid configuration accepted")
			}
			if calls != 0 || backend.calls != 0 {
				t.Fatal("invalid numeric input reached backend")
			}
		})
	}
}

func TestInvalidCLIRatioFailsBeforeScanning(t *testing.T) {
	command := newScanCommand()
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"--ratio", strconv.Itoa(int(^uint(0)>>1)) + "0:1", "/unavailable-panora-test-directory"})
	err := command.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "aspect ratio") {
		t.Fatalf("ratio was not rejected before filesystem access: %v", err)
	}
}
