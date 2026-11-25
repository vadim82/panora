package cli

import "github.com/spf13/cobra"

// NewRootCommand builds the CLI for the service, controls, and diagnostics.
func NewRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "panora",
		Short:         "Automatic wallpaper discovery and rotation",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	rootCmd.AddCommand(
		newScanCommand(), newConfigCommand(), newProviderCommand(), newDebugCommand(),
		newControlCommand("next", "Advance through forward history, then choose a random eligible image"),
		newDoctorCommand(), newServeCommand(nil), newStopCommand(),
		newControlCommand("status", "Read service playback status without changing the desktop"),
		newControlCommand("previous", "Return to an earlier available image in service history"),
		newControlCommand("pause", "Pause automatic rotation; manual next/previous remain available"),
		newControlCommand("resume", "Resume automatic rotation with a fresh interval"),
		newExclusionsCommand(), newHistoryCommand(), newShellCommand(),
	)
	installHelp(rootCmd)
	installArgumentErrors(rootCmd)
	return rootCmd
}
