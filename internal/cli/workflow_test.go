package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/playback"
)

func executeTest(args ...string) (string, string, error) {
	var out, errOut bytes.Buffer
	err := Execute(context.Background(), args, &out, &errOut)
	return out.String(), errOut.String(), err
}

func TestCompactStatusAndActionOutput(t *testing.T) {
	next := time.Date(2026, 10, 26, 15, 0, 0, 0, time.UTC)
	status := control.Status{
		Version: 1, Paused: true, Interval: "30m0s", NextAt: &next,
		Providers: []playback.ProviderRefreshHealth{{ID: "online", State: "failed", Retries: []playback.RetryHealth{{Scope: "provider_metadata", Attempts: 1}}, Inputs: []playback.InputRefreshHealth{{ID: "search", State: "failed", Interval: "5m0s"}}}},
		Uncertain: true, LastError: &control.Error{Code: "apply_uncertain", Message: "inspect desktop"},
		Warnings: []string{"Cached playback is available."},
	}
	cmd := newControlCommand("status", "Status")
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := writeControlStatus(cmd, status, false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"paused (every 30m)", "Last accepted image: none", "1 failed", "uncertain", "inspect desktop", "Cached playback", "Provider backoff"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, &out)
		}
	}
	if strings.Contains(out.String(), "search:") || strings.Contains(out.String(), "cursor") {
		t.Fatal("compact status leaked details")
	}
	out.Reset()
	cmd.Flags().Set("verbose", "true")
	if err := writeControlStatus(cmd, status, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "search: failed") {
		t.Fatalf("verbose details absent: %s", &out)
	}
	out.Reset()
	status = control.Status{Version: 1, Paused: true, Interval: "30m0s"}
	if err := writeActionResult(cmd, status, "pause", false); err != nil {
		t.Fatal(err)
	}
	if out.String() != "Rotation paused.\n" {
		t.Fatalf("action output: %q", out.String())
	}
	out.Reset()
	status.Current = &playback.Entry{Path: "lake\x1b[2J\n.png", SourceID: "home"}
	if err := writeActionResult(cmd, status, "next", false); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(out.String(), "\x1b") || strings.Count(out.String(), "\n") != 1 || !strings.Contains(out.String(), "accepted") {
		t.Fatalf("unsafe or inaccurate action output: %q", out.String())
	}
	out.Reset()
	if err := writeControlStatus(cmd, status, true); err != nil {
		t.Fatal(err)
	}
	var decoded control.Status
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil || decoded.Current.Path != status.Current.Path {
		t.Fatalf("JSON changed: %s %v", &out, err)
	}
}

func TestJSONErrorsIncludeParserFailuresAndNoDuplicateStderr(t *testing.T) {
	for _, args := range [][]string{
		{"next", "--unknown", "--json"},
		{"next", "extra", "--json"},
		{"next", "--socket", "relative", "--json"},
		{"status", "--json=invalid"},
		{"exclusions", "remove", "bad", "--json"},
		{"exclusions", "ls", "--limit", "bad", "--json"},
		{"exclusions", "undo", "--json"},
		{"config", "check", "--config", "/missing/config.toml", "--json"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, errOut, err := executeTest(args...)
			if err == nil || errOut != "" {
				t.Fatalf("err=%v stderr=%q stdout=%q", err, errOut, out)
			}
			var result struct {
				Version int            `json:"version"`
				Error   *control.Error `json:"error"`
			}
			decoder := json.NewDecoder(strings.NewReader(out))
			if err := decoder.Decode(&result); err != nil || result.Version != 1 || result.Error == nil || result.Error.Code == "" {
				t.Fatalf("invalid error document: %q %v", out, err)
			}
			if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
				t.Fatalf("extra output: %q", out)
			}
		})
	}
	out, errOut, err := executeTest("next", "--unknown")
	if err == nil || out != "" || !strings.Contains(errOut, "Error:") || !strings.Contains(errOut, "--help") {
		t.Fatalf("human error: %q %q %v", out, errOut, err)
	}
	// A flag value or a positional after -- is not a JSON output request.
	for _, args := range [][]string{{"next", "--socket", "--json"}, {"next", "--", "--json"}, {"next", "extra", "--json=false"}} {
		out, errOut, err := executeTest(args...)
		if err == nil || out != "" || errOut == "" {
			t.Fatalf("incorrect mode for %v: %q %q %v", args, out, errOut, err)
		}
	}
}

func TestReadOnlyConfigCommands(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	path, err := playback.ConfigPath("")
	if err != nil {
		t.Fatal(err)
	}
	out, errOut, err := executeTest("config", "path")
	if err != nil || errOut != "" || out != path+"\n" {
		t.Fatalf("path: %q %q %v", out, errOut, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("path created configuration")
	}
	config := writeDesktopConfig(t, "auto")
	before, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	// A missing input is not a schema-validation failure.
	saved, _, err := playback.LoadConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(saved.Providers[0].Folders[0].Path); err != nil {
		t.Fatal(err)
	}
	fakeBackendCommands(t, map[string]string{"omarchy": "exit 99"})
	out, errOut, err = executeTest("config", "check", "--config", config, "--json")
	if err != nil || errOut != "" || !strings.Contains(out, `"valid":true`) {
		t.Fatalf("check: %q %q %v", out, errOut, err)
	}
	after, err := os.ReadFile(config)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("config check changed file")
	}
	if err := os.WriteFile(config, []byte("version=1\nunknown=true"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := executeTest("config", "check", "--config", config); err == nil {
		t.Fatal("accepted invalid config")
	}
}

func TestScanUsesSavedRulesAndExplicitOverrides(t *testing.T) {
	root := t.TempDir()
	writeScanImage(t, filepath.Join(root, "small.png"), 2, 2)
	config := writeNextConfig(t, root)
	body, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	out, errOut, err := executeTest("scan", root, "--config", config)
	if err != nil || errOut != "" || !strings.Contains(out, "minimum 2x2") || !strings.Contains(out, "1 passed") {
		t.Fatalf("saved rules: %q %q %v", out, errOut, err)
	}
	out, _, err = executeTest("scan", root, "--config", config, "--min-width", "1920")
	if err != nil || !strings.Contains(out, "1 rejected") || !strings.Contains(out, "below minimum 1920x2") {
		t.Fatalf("override: %q %v", out, err)
	}
	after, _ := os.ReadFile(config)
	if !bytes.Equal(body, after) {
		t.Fatal("scan changed config")
	}
	if err := os.WriteFile(filepath.Join(root, "bad.png"), []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	out, errOut, err = executeTest("scan", root, "--config", config)
	if err == nil || !strings.Contains(out, "1 unreadable") || !strings.Contains(errOut, "Warning:") {
		t.Fatalf("unreadable: %q %q %v", out, errOut, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := playback.ScanDirectoryContext(ctx, root, 0, 1, 1, nil, func(playback.ScanResult) error { t.Fatal("visited after cancellation"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestHelpGroupsAndRemovedCommands(t *testing.T) {
	out, errOut, err := executeTest("--help")
	if err != nil || errOut != "" {
		t.Fatalf("help: %q %v", errOut, err)
	}
	for _, label := range []string{"Playback controls:", "Foreground service:", "Diagnostics and configuration:", "Advanced and internal commands:", "Examples:"} {
		if !strings.Contains(out, label) {
			t.Fatalf("help missing %q: %s", label, out)
		}
	}
	if strings.Contains(out, "\x1b") || strings.Contains(out, "--backend") {
		t.Fatalf("unexpected style/flag: %q", out)
	}
	for _, args := range [][]string{{"exclude"}, {"undo-exclude"}, {"restore-exclusion", "1"}, {"set", "x.png"}, {"backend"}, {"serve", "--backend", "auto"}, {"exclusions", "undo"}} {
		if _, _, err := executeTest(args...); err == nil {
			t.Fatalf("removed command accepted: %v", args)
		}
	}
	cmd := &cobra.Command{}
	cmd.Flags().Bool("json", false, "JSON output")
	if jsonRequested(cmd, []string{"--", "--json"}) {
		t.Fatal("literal JSON flag recognized")
	}
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	if strings.Contains(styled(os.Stdout, "31", "Error:"), "\x1b") {
		t.Fatal("NO_COLOR ignored")
	}
}

func TestDoctorReportsPartialFailureWithoutApplyingOrWriting(t *testing.T) {
	config := writeDesktopConfig(t, "noctalia")
	before, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "desktop-mutation")
	t.Setenv("PANORA_TEST_MARKER", marker)
	fakeBackendCommands(t, map[string]string{
		"noctalia": `case "$1 $2" in
"msg wallpaper-get") printf '/before.png\n' ;;
"config export") printf '[wallpaper]\nenabled=true\n' ;;
*) printf changed > "$PANORA_TEST_MARKER" ;;
esac`,
		"omarchy":       "exit 9",
		"omarchy-shell": `printf probed > "$PANORA_TEST_MARKER"; echo ok`,
	})
	out, errOut, err := executeTest("doctor", "--config", config, "--socket", shortSocket(t), "--json")
	var report doctorReport
	if err != nil || errOut != "" || json.Unmarshal([]byte(out), &report) != nil || report.ResolvedBackend != "noctalia" || report.Error != nil {
		t.Fatalf("doctor: %q %q %v", out, errOut, err)
	}
	if len(report.Checks) != 3 || report.Checks[2].State != "warning" {
		t.Fatalf("missing-service warning: %+v", report)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("doctor mutated desktop or probed unselected integration")
	}
	after, _ := os.ReadFile(config)
	if !bytes.Equal(before, after) {
		t.Fatal("doctor wrote configuration")
	}
	fakeBackendCommands(t, map[string]string{"noctalia": "exit 9"})
	out, errOut, err = executeTest("doctor", "--config", config, "--socket", shortSocket(t), "--json")
	if err == nil || errOut != "" || json.Unmarshal([]byte(out), &report) != nil || report.Error == nil || len(report.Checks) != 3 {
		t.Fatalf("partial diagnostic failure: %q %q %v", out, errOut, err)
	}
	if err := os.WriteFile(config, []byte("version='invalid'"), 0600); err != nil {
		t.Fatal(err)
	}
	out, errOut, err = executeTest("doctor", "--config", config, "--socket", shortSocket(t), "--json")
	if err == nil || errOut != "" || json.Unmarshal([]byte(out), &report) != nil || len(report.Checks) != 3 || report.Checks[1].State != "warning" {
		t.Fatalf("invalid-config diagnostic: %q %q %v", out, errOut, err)
	}
}

type failingOutput struct{ err error }

func (out failingOutput) Write([]byte) (int, error) { return 0, out.err }

func TestFormattedOutputPreservesWriteErrors(t *testing.T) {
	want := errors.New("output unavailable")
	cmd := newControlCommand("status", "Status")
	cmd.SetOut(failingOutput{want})
	status := control.Status{Version: 1, Paused: true, Interval: "30m"}
	if err := writeControlStatus(cmd, status, false); !errors.Is(err, want) {
		t.Fatalf("status write error: %v", err)
	}
	if err := writeActionResult(cmd, status, "pause", false); !errors.Is(err, want) {
		t.Fatalf("action write error: %v", err)
	}
}
