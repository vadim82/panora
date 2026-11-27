package source

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/vadim82/panora/internal/filelock"
	"github.com/vadim82/panora/internal/localfiles"
)

const (
	maxInstalledProviders = 16
	maxExecutableBytes    = 64 << 20
)

const AlphaCodersID = "alpha-coders"

var externalID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)

// ValidExternalID excludes the bundled provider namespace.
func ValidExternalID(id string) bool {
	return id != "local" && id != "wallhaven" && id != "omarchy" && id != AlphaCodersID && externalID.MatchString(id)
}

// InstalledPlugin represents a registry entry, including one that needs repair.
type InstalledPlugin struct {
	ID     string
	Client *Client
	Err    error
}

// ValidInputProviderID includes built-in providers that use the shared input editor.
func ValidInputProviderID(id string) bool { return id == AlphaCodersID || ValidExternalID(id) }

func PluginDirectory() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(directory) {
		return "", fmt.Errorf("plugin registry requires an absolute configuration directory")
	}
	return filepath.Join(directory, "panora", "providers"), nil
}

func loadExternal(directory string) (*Client, error) {
	for _, name := range []string{directory, filepath.Join(directory, "plugin.json")} {
		info, err := os.Lstat(name)
		if err != nil {
			return nil, err
		}
		if name == directory && !info.IsDir() || name != directory && !info.Mode().IsRegular() {
			return nil, fmt.Errorf("expected a regular plugin directory and manifest")
		}
	}
	client, err := loadManifest(directory)
	if err != nil {
		return nil, err
	}
	manifest := client.Manifest
	if !ValidExternalID(manifest.ID) || len(manifest.Name) > 128 || strings.ContainsAny(manifest.Name, "\x00\r\n") {
		return nil, fmt.Errorf("invalid provider ID or display name")
	}
	executable := manifest.Command[0]
	if executable == "." || executable == ".." || executable == "plugin.json" || filepath.Base(executable) != executable {
		return nil, fmt.Errorf("provider command must name an executable in the plugin directory")
	}
	client.bindCommand()
	file, err := os.Lstat(client.Manifest.Command[0])
	if err != nil || !file.Mode().IsRegular() || file.Mode().Perm()&0100 == 0 || file.Size() > maxExecutableBytes {
		return nil, fmt.Errorf("provider command must be a regular executable of at most 64 MiB")
	}
	return client, nil
}

func InstalledPlugins(root string) ([]InstalledPlugin, error) {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]InstalledPlugin, 0, min(len(entries), maxInstalledProviders))
	for _, entry := range entries {
		id := entry.Name()
		if !ValidExternalID(id) {
			continue
		}
		if len(result) == maxInstalledProviders {
			return result, fmt.Errorf("provider registry supports up to %d providers", maxInstalledProviders)
		}
		client, err := loadExternal(filepath.Join(root, id))
		if err == nil && client.Manifest.ID != id {
			client, err = nil, fmt.Errorf("provider ID must match its installed directory")
		}
		result = append(result, InstalledPlugin{ID: id, Client: client, Err: err})
	}
	return result, nil
}

func InstallPlugin(ctx context.Context, root, directory string) (string, error) {
	directory, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	client, err := loadExternal(directory)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	release, err := filelock.Acquire(ctx, filepath.Join(root, ".install.lock"))
	if err != nil {
		return "", fmt.Errorf("lock provider registry: %w", err)
	}
	defer release()
	registered, err := InstalledPlugins(root)
	if err != nil {
		return "", err
	}
	id := client.Manifest.ID
	for _, existing := range registered {
		if existing.ID == id {
			return "", fmt.Errorf("provider %q is already installed", id)
		}
	}
	if len(registered) == maxInstalledProviders {
		return "", fmt.Errorf("provider registry supports up to %d providers", maxInstalledProviders)
	}
	stage, err := os.MkdirTemp(root, ".install-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	executable := filepath.Base(client.Manifest.Command[0])
	if err := copyPluginFile(directory, stage, "plugin.json", 0600, MaxRequestBytes); err != nil {
		return "", err
	}
	if err := copyPluginFile(directory, stage, executable, 0700, maxExecutableBytes); err != nil {
		return "", err
	}
	copied, err := loadExternal(stage)
	if err != nil {
		return "", err
	}
	if copied.Manifest.ID != id || filepath.Base(copied.Manifest.Command[0]) != executable {
		return "", fmt.Errorf("provider manifest changed during installation")
	}
	copied.Timeout = 3 * time.Second
	if _, err := copied.Describe(ctx); err != nil {
		return "", fmt.Errorf("provider %q did not describe a compatible implementation: %w", id, err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.Rename(stage, filepath.Join(root, id)); err != nil {
		return "", fmt.Errorf("publish provider %q: %w", id, err)
	}
	return id, nil
}

func copyPluginFile(source, destination, name string, mode os.FileMode, limit int64) error {
	in, err := localfiles.OpenRegular(filepath.Join(source, name))
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(filepath.Join(destination, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(out, io.LimitReader(in, limit+1))
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if written > limit {
		return fmt.Errorf("provider file exceeds install limit")
	}
	return closeErr
}
