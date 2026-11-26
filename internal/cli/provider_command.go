package cli

import (
	"github.com/spf13/cobra"

	"github.com/vadim82/panora/internal/source"
	"github.com/vadim82/panora/internal/sources/commons"
	"github.com/vadim82/panora/internal/sources/local"
	"github.com/vadim82/panora/internal/sources/wallhaven"
)

func newProviderCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "provider", Short: "Run a bundled provider or install a provider program"}
	cmd.AddCommand(&cobra.Command{
		Use: "local", Short: "Handle a local-folder provider request",
		Long: "Run the bundled local-files provider. Panora starts this command automatically.\nDirect use expects a JSON-RPC 2.0 request on stdin and writes its reply to stdout.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return source.Serve(cmd.Context(), local.Provider{}, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "wallhaven", Short: "Handle a Wallhaven metadata request", Args: cobra.NoArgs,
		Long: "Run the bundled Wallhaven provider. Expects a JSON-RPC 2.0 request on stdin.\nReturns public SFW image metadata on stdout; no originals are downloaded.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return source.Serve(cmd.Context(), wallhaven.New(), cmd.InOrStdin(), cmd.OutOrStdout())
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "commons", Short: "Handle a Wikimedia Commons metadata request", Args: cobra.NoArgs,
		Long: "Run the bundled Wikimedia Commons provider. Expects a JSON-RPC 2.0 request on stdin.\nReturns reviewed landscape metadata without downloading originals.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return source.Serve(cmd.Context(), commons.New(), cmd.InOrStdin(), cmd.OutOrStdout())
		},
	})
	cmd.AddCommand(newProviderInstallCommand())
	return cmd
}
