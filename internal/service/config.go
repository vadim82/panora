package service

import (
	"fmt"
	"io"

	"github.com/vadim82/panora/internal/playback"
)

// PrepareConfig bootstraps only the implicit user config. Explicit paths and
// existing files are validated by the loader, never replaced with defaults.
func PrepareConfig(path string, out io.Writer, omarchySession bool) (string, bool, error) {
	resolved, err := playback.ConfigPath(path)
	if err != nil || path != "" {
		return resolved, false, err
	}
	created, err := playback.InitializeDefaultConfig(resolved, omarchySession)
	if err != nil {
		return "", false, fmt.Errorf("create default configuration: %w", err)
	}
	if created {
		message := "Panora will wait for a folder or search."
		if omarchySession {
			message = "Panora will start with the active Omarchy theme."
		}
		_, err = fmt.Fprintf(out, "Created default settings at %q. %s\n", resolved, message)
	}
	return resolved, created, err
}
