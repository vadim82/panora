package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/playback"
)

func newSetCommand(resolve func(context.Context) (desktop.WallpaperBackend, error)) *cobra.Command {
	var options desktop.DisplayOptions
	var configPath string
	setCmd := &cobra.Command{
		Use:   "set IMAGE",
		Short: "Set a local image as wallpaper",
		Long: `Set a local image as wallpaper.

IMAGE is the path to a local image file.
Paths containing spaces must be quoted.

The image header is inspected before applying it through the selected
wallpaper backend. That backend must be available in your desktop session.

The desktop integration and presentation defaults come from config.toml.
Use panora doctor to inspect readiness. Explicit --fit/--upscale options affect
only this diagnostic. This directly changes the desktop without playback
history or eligibility rules; a running service may replace it on its next change.
Originals are never overwritten.`,
		Example: `  panora debug set ~/Pictures/wallpaper.jpg
  panora debug set "Pictures/my wallpaper.png"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := options.Validate(); err != nil {
				return err
			}
			path, err := filepath.Abs(args[0])
			if err != nil {
				return fmt.Errorf("resolve image path: %w", err)
			}

			info, err := playback.ReadImageInfo(path)
			if err != nil {
				return err
			}

			resolver := resolve
			if resolver == nil {
				config, _, err := playback.LoadConfig(configPath)
				if err != nil {
					return &commandError{code: "config_invalid", err: err}
				}
				options = configuredDisplayOptions(cmd, config.Desktop, options)
				if err := options.Validate(); err != nil {
					return argumentError(cmd, err)
				}
				resolver = func(ctx context.Context) (desktop.WallpaperBackend, error) {
					selected, err := desktop.Select(ctx, config.Desktop.Backend)
					return selected.Backend, err
				}
			}

			backend, err := resolver(cmd.Context())
			if err != nil {
				return err
			}
			if err := backend.SetWallpaper(cmd.Context(), info.Path, options); err != nil {
				return err
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Wallpaper request accepted: %s\n", terminalText(info.Path))
			return err
		},
	}
	addConfigFlag(setCmd, &configPath)
	setCmd.Flags().StringVar(&options.Fit, "fit", "auto", "display mode: auto, contain (centered Fit), or crop")
	setCmd.Flags().BoolVar(&options.Upscale, "upscale", false, "allow enlarging smaller images in Omarchy/macOS Fit mode")
	return setCmd
}

func newDebugCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "debug", Short: "Run explicit desktop diagnostics; set changes the wallpaper"}
	cmd.AddCommand(newSetCommand(nil))
	return cmd
}
