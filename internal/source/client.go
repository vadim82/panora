package source

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync/atomic"
	"time"

	"github.com/vadim82/panora/internal/localfiles"
)

type Manifest struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Version  string          `json:"version"`
	Protocol int             `json:"protocol"`
	Command  []string        `json:"command"`
	Editor   json.RawMessage `json:"editor,omitempty"`
}

// Client launches one process per operation. Command[0] must be absolute;
// arguments are never interpreted by a shell. Timeout may shorten the 30s cap.
type Client struct {
	Manifest  Manifest
	Directory string
	Timeout   time.Duration
}

func BundledLocal() *Client {
	executable, _ := os.Executable()
	return &Client{Manifest: Manifest{ID: "local", Name: "Local folder", Version: "0.1.0", Protocol: Protocol, Command: []string{executable, "provider", "local"}}}
}

// Load explicitly registers one local implementation. No PATH/plugin scanning.
func Load(directory string) (*Client, error) {
	client, err := loadManifest(directory)
	if err != nil {
		return nil, err
	}
	if client.Manifest.ID != "local" {
		return nil, fmt.Errorf("expected a local plugin")
	}
	client.bindCommand()
	return client, nil
}

func (c *Client) bindCommand() {
	if !filepath.IsAbs(c.Manifest.Command[0]) {
		c.Manifest.Command[0] = filepath.Join(c.Directory, c.Manifest.Command[0])
	}
}

func loadManifest(directory string) (*Client, error) {
	if !filepath.IsAbs(directory) {
		return nil, fmt.Errorf("plugin directory must be absolute")
	}
	file, err := localfiles.OpenRegular(filepath.Join(directory, "plugin.json"))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxRequestBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxRequestBytes {
		return nil, fmt.Errorf("plugin manifest too large")
	}
	var manifest Manifest
	if err := Decode(data, &manifest); err != nil {
		return nil, fmt.Errorf("plugin manifest: %w", err)
	}
	if manifest.ID == "" || manifest.Name == "" || manifest.Version == "" || len(manifest.Command) == 0 || manifest.Command[0] == "" {
		return nil, fmt.Errorf("expected a named, versioned plugin with a command")
	}
	if manifest.Protocol != Protocol {
		return nil, fmt.Errorf("unsupported plugin protocol %d; expected 2", manifest.Protocol)
	}
	return &Client{Manifest: manifest, Directory: directory}, nil
}

var requestID atomic.Uint64

var jobs = make(chan struct{}, 2)

type boundedBuffer struct {
	bytes.Buffer
	limit    int
	cancel   context.CancelFunc
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.limit - b.Len()
	if len(p) > remaining {
		b.Buffer.Write(p[:remaining])
		b.overflow = true
		if b.cancel != nil {
			b.cancel()
		}
		return n, nil // Discard excess while the process is canceled/reaped.
	}
	b.Buffer.Write(p)
	return n, nil
}

func (c *Client) call(ctx context.Context, op string, params, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.Manifest.Protocol != Protocol || len(c.Manifest.Command) == 0 || !filepath.IsAbs(c.Manifest.Command[0]) {
		return fmt.Errorf("invalid source plugin command or protocol")
	}
	timeout := c.Timeout
	if timeout <= 0 || timeout > 30*time.Second {
		timeout = 30 * time.Second
	}
	opCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case jobs <- struct{}{}:
		defer func() { <-jobs }()
	case <-opCtx.Done():
		return opCtx.Err()
	}
	id := fmt.Sprintf("req-%d", requestID.Add(1))
	paramData, err := json.Marshal(params)
	if err != nil {
		return err
	}
	var named map[string]json.RawMessage
	if err := json.Unmarshal(paramData, &named); err != nil || named == nil {
		return fmt.Errorf("provider parameters must be an object")
	}
	named["protocol"], _ = json.Marshal(Protocol)
	paramData, _ = json.Marshal(named)
	encodedID, _ := json.Marshal(id)
	data, err := json.Marshal(Request{JSONRPC: JSONRPC, ID: encodedID, Method: op, Params: paramData})
	if err != nil {
		return err
	}
	if len(data)+1 > MaxRequestBytes {
		return fmt.Errorf("source request too large")
	}
	command := exec.CommandContext(opCtx, c.Manifest.Command[0], c.Manifest.Command[1:]...)
	command.Dir = c.Directory
	command.Env = pluginEnvironment()
	command.Stdin = bytes.NewReader(append(data, '\n'))
	stdout := &boundedBuffer{limit: MaxResponseBytes, cancel: cancel}
	stderr := &boundedBuffer{limit: 64 << 10}
	command.Stdout, command.Stderr = stdout, stderr
	command.WaitDelay = 250 * time.Millisecond
	configureProcess(command)
	err = command.Run()
	cleanupProcess(command)
	if stdout.overflow {
		return fmt.Errorf("source %s: response exceeds %d bytes", c.Manifest.ID, MaxResponseBytes)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if opCtx.Err() != nil {
		return fmt.Errorf("source %s %s: %w", c.Manifest.ID, op, opCtx.Err())
	}
	if err != nil {
		return fmt.Errorf("source %s %s failed: %w (plugin stderr withheld)", c.Manifest.ID, op, err)
	}
	line := stdout.Bytes()
	if len(line) == 0 || line[len(line)-1] != '\n' || bytes.Contains(line[:len(line)-1], []byte{'\n'}) {
		return fmt.Errorf("source %s: expected exactly one response line", c.Manifest.ID)
	}
	response, err := decodeResponse(line, id)
	if err != nil {
		return fmt.Errorf("provider %s %s: %w", c.Manifest.ID, op, err)
	}
	if err := json.Unmarshal(response.Result, result); err != nil {
		return fmt.Errorf("source %s: malformed %s result", c.Manifest.ID, op)
	}
	return nil
}

func validErrorCode(code string) bool {
	switch code {
	case "invalid_config", "auth_required", "auth_rejected", "rate_limited", "unavailable", "invalid_cursor", "not_found", "unsupported", "internal":
		return true
	}
	return false
}

func pluginEnvironment() []string {
	var env []string
	for _, key := range []string{"HOME", "PATH", "LANG", "LC_ALL", "TMPDIR", "SYSTEMROOT", "GORACE"} {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return env
}

func (c *Client) Describe(ctx context.Context) (Description, error) {
	var result Description
	err := c.call(ctx, "describe", struct{}{}, &result)
	if err == nil && (result.Protocol != Protocol || result.ID != c.Manifest.ID || len(result.Requirements) != 0 || !slices.Contains(result.MediaKinds, "image")) {
		err = fmt.Errorf("incompatible provider description; expected protocol 2 and image support")
	}
	if err == nil {
		for _, method := range []string{"describe", "validate", "discover", "resolve"} {
			if !slices.Contains(result.Operations, method) {
				return result, fmt.Errorf("provider does not support %s", method)
			}
		}
	}
	return result, err
}
func (c *Client) Validate(ctx context.Context, config json.RawMessage) error {
	var result struct{}
	return c.call(ctx, "validate", struct {
		Config json.RawMessage `json:"config"`
	}{config}, &result)
}
func (c *Client) Discover(ctx context.Context, params DiscoverParams) (Page, error) {
	if len(params.Cursor) == 0 {
		params.Cursor = json.RawMessage("null")
	}
	var result Page
	err := c.call(ctx, "discover", params, &result)
	if err == nil {
		err = validatePage(result, params.Limit)
	}
	return result, err
}
func (c *Client) Resolve(ctx context.Context, params ResolveParams) (Resolved, error) {
	var result Resolved
	err := c.call(ctx, "resolve", params, &result)
	if err == nil {
		err = result.Validate()
	}
	return result, err
}

func (c *Client) Suggest(ctx context.Context, params SuggestParams) ([]string, error) {
	if !params.Valid() {
		return nil, fmt.Errorf("invalid suggestion request")
	}
	var result suggestionResult
	if err := c.call(ctx, SuggestMethod, params, &result); err != nil {
		return nil, err
	}
	if !result.valid(params.Limit) {
		return nil, fmt.Errorf("provider returned invalid suggestions")
	}
	return result.Values, nil
}
