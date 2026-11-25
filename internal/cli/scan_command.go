package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/playback"
)

func newScanCommand() *cobra.Command {
	var configPath, ratioText string
	var ratioPresets []string
	var maxDepth, minWidth, minHeight int
	cmd := &cobra.Command{
		Use: "scan DIRECTORY", Short: "Diagnose local image headers using saved image rules",
		Long:    "Inspect local JPEG, PNG, and static WebP headers and explain which saved image rules they meet.\nThis optional diagnostic never enrolls images, refreshes providers or changes playback.\nExplicit flags override saved rules for this scan only. A valid config is required.",
		Example: "  panora scan ~/Pictures/Wallpapers\n  panora scan ./pictures --min-width 1920 --min-height 1080",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Reject explicit malformed ratios before accessing config or scan files.
			var overrideRatio *playback.RatioRule
			if cmd.Flags().Changed("ratio-presets") {
				if cmd.Flags().Changed("ratio") && ratioText != "presets" {
					return argumentError(cmd, fmt.Errorf("ratio-presets requires ratio presets"))
				}
				ratioText = "presets"
			}
			override := cmd.Flags().Changed("ratio") || cmd.Flags().Changed("ratio-presets")
			if override {
				parsed, err := playback.ParseRatioRule(ratioText, ratioPresets)
				if err != nil {
					return argumentError(cmd, err)
				}
				overrideRatio = parsed
			}
			config, ratio, err := playback.LoadConfig(configPath)
			if err != nil {
				return &commandError{code: "config_invalid", err: err}
			}
			if !cmd.Flags().Changed("max-depth") {
				maxDepth = config.Filters.MaxDepth
			}
			if !cmd.Flags().Changed("min-width") {
				minWidth = config.Filters.MinWidth
			}
			if !cmd.Flags().Changed("min-height") {
				minHeight = config.Filters.MinHeight
			}
			if override {
				ratio = overrideRatio
			} else {
				ratioText = config.Filters.Ratio
			}
			if maxDepth < 0 || minWidth < 1 || minHeight < 1 {
				return argumentError(cmd, fmt.Errorf("depth must be non-negative and minimum dimensions must be positive"))
			}
			if ratioText == "monitor" || ratioText == "monitor-similar" {
				capabilities, err := desktop.InspectCapabilities(cmd.Context(), config.Desktop.Backend)
				if err != nil {
					return fmt.Errorf("resolve Match monitor: %w", err)
				}
				var resolution playback.RuleResolution
				ratio, resolution = playback.ResolveRatioRule(ratio, capabilities)
				if resolution.State != "resolved" {
					return fmt.Errorf("Match monitor is unavailable: %s", resolution.Reason)
				}
				ratioText += " (" + resolution.Ratio + ")"
			}
			out := cmd.OutOrStdout()
			if _, err := fmt.Fprintf(out, "Image rules: minimum %dx%d, ratio %s, depth %d\n", minWidth, minHeight, terminalText(ratioText), maxDepth); err != nil {
				return err
			}
			var passed, rejected, unreadable int
			err = playback.ScanDirectoryContext(cmd.Context(), args[0], maxDepth, minWidth, minHeight, ratio, func(result playback.ScanResult) error {
				switch {
				case result.Err != nil:
					unreadable++
					_, err := fmt.Fprintf(cmd.ErrOrStderr(), "%s %s\n", styled(cmd.ErrOrStderr(), "33", "Warning:"), terminalText(result.Err.Error()))
					return err
				case len(result.Reasons) == 0:
					passed++
					_, err := fmt.Fprintf(out, "PASS %s\n", terminalText(result.Image.String()))
					return err
				default:
					rejected++
					_, err := fmt.Fprintf(out, "REJECT %s — %s\n", terminalText(result.Image.String()), terminalText(strings.Join(result.Reasons, "; ")))
					return err
				}
			})
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(out, "Inspected %d headers: %d passed, %d rejected, %d unreadable.\nFull decoding and rendering were not checked.\n", passed+rejected+unreadable, passed, rejected, unreadable); err != nil {
				return err
			}
			if unreadable > 0 {
				return fmt.Errorf("could not inspect %d image headers", unreadable)
			}
			return nil
		},
	}
	addConfigFlag(cmd, &configPath)
	cmd.Flags().IntVar(&maxDepth, "max-depth", 0, "maximum subfolder depth (omitted: saved rule)")
	cmd.Flags().IntVar(&minWidth, "min-width", 0, "minimum image width (omitted: saved rule)")
	cmd.Flags().StringSliceVar(&ratioPresets, "ratio-presets", nil, "accepted presets, comma separated (implies --ratio presets)")
	cmd.Flags().IntVar(&minHeight, "min-height", 0, "minimum image height (omitted: saved rule)")
	cmd.Flags().StringVar(&ratioText, "ratio", "", "aspect ratio width:height, any, monitor, monitor-similar, or presets (omitted: saved rule)")
	return cmd
}
