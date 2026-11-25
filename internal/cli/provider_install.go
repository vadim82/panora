package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/vadim82/panora/internal/source"
)

func newProviderInstallCommand() *cobra.Command {
	return &cobra.Command{
		Use: "install DIRECTORY", Short: "Install a trusted provider program", Args: cobra.ExactArgs(1),
		Long: "Install a provider from its local directory. Panora runs the supplied program as your user to verify compatibility. Restart the service to load the new provider.",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := source.PluginDirectory()
			if err != nil {
				return err
			}
			id, err := source.InstallPlugin(cmd.Context(), root, args[0])
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Installed %s. Restart Panora's service to load it.\n", id)
			return err
		},
	}
}
