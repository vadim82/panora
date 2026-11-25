package cli

import (
	"github.com/spf13/cobra"
	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/playback"
)

func configuredDisplayOptions(cmd *cobra.Command, saved playback.DesktopConfig, overrides desktop.DisplayOptions) desktop.DisplayOptions {
	options := saved.Options()
	if cmd.Flags().Changed("fit") {
		options.Fit = overrides.Fit
	}
	if cmd.Flags().Changed("upscale") {
		options.Upscale = overrides.Upscale
	}
	return options
}
