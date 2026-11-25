package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vadim82/panora/internal/control"
)

func newStopCommand() *cobra.Command {
	var socket, instance string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use: "stop", Short: "Stop the running service; leave its last accepted wallpaper in place", Args: cobra.NoArgs,
		Annotations: map[string]string{"control": "true"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := control.SocketPath(socket)
			if err != nil {
				return err
			}
			result, err := control.ShutdownInstance(cmd.Context(), path, instance)
			if err != nil {
				return err
			}
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "Panora stop requested.")
			return err
		},
	}
	cmd.Flags().StringVar(&socket, "socket", "", "private control socket (default: platform runtime directory/panora/control.sock)")
	cmd.Flags().StringVar(&instance, "instance", "", "request shutdown only from the given service instance")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "print the versioned service response as JSON")
	return cmd
}
