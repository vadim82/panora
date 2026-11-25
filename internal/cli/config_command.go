package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/playback"
)

func addConfigFlag(cmd *cobra.Command, path *string) {
	cmd.Flags().StringVar(path, "config", "", "configuration file (default: OS user config directory/panora/config.toml)")
}

func newConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "config", Short: "Locate or validate saved settings without changing them",
		Example: "  panora config path\n  panora config check --config ./config.toml",
	}
	for _, name := range []string{"path", "check"} {
		var configPath string
		var jsonOutput bool
		child := &cobra.Command{
			Use: name, Short: "Print the selected configuration path", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				path, err := playback.ConfigPath(configPath)
				if err == nil && name == "check" {
					_, _, err = playback.LoadConfig(path)
				}
				if err != nil {
					return &commandError{code: "config_invalid", err: err}
				}
				if jsonOutput {
					return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
						Version int    `json:"version"`
						Path    string `json:"path"`
						Valid   bool   `json:"valid,omitempty"`
					}{control.APIVersion, path, name == "check"})
				}
				message := terminalText(path)
				if name == "check" {
					message = "Configuration valid: " + message
				}
				_, err = fmt.Fprintln(cmd.OutOrStdout(), message)
				return err
			},
		}
		if name == "check" {
			child.Short = "Validate the configuration file without starting playback"
		}
		addConfigFlag(child, &configPath)
		child.Flags().BoolVar(&jsonOutput, "json", false, "print a versioned JSON result")
		cmd.AddCommand(child)
	}
	return cmd
}
