package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/playback"
)

func newExclusionsCommand() *cobra.Command {
	var socket string
	var jsonOutput bool
	cmd := &cobra.Command{
		Annotations: map[string]string{"control": "true"},
		Use:         "exclusions", Short: "List, add or remove wallpaper exclusions",
		Long:    "Manage exclusions without deleting image files or changing the wallpaper.\nWith no subcommand, list the newest exclusions.",
		Example: "  panora exclusions ls\n  panora exclusions add\n  panora exclusions remove 12",
	}
	cmd.PersistentFlags().StringVar(&socket, "socket", "", "private service control socket")
	cmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "print the versioned service response as JSON")
	list := newExclusionsListCommand(&socket, &jsonOutput)
	cmd.RunE, cmd.Args = list.RunE, cobra.NoArgs
	// Bare exclusions is the same bounded first-page list; paging belongs on ls.
	cmd.AddCommand(list, &cobra.Command{
		Use: "add", Short: "Exclude the current image from future playback", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runControlCommand(cmd, socket, "exclude", "", jsonOutput)
		},
	}, newExclusionsRemoveCommand(&socket, &jsonOutput))
	return cmd
}

func newExclusionsListCommand(socket *string, jsonOutput *bool) *cobra.Command {
	var before int64
	var limit int
	cmd := &cobra.Command{
		Use: "ls", Short: "List excluded images and their removal IDs", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := control.SocketPath(*socket)
			if err != nil {
				return err
			}
			page, err := control.ListExclusions(cmd.Context(), path, before, limit)
			if err != nil {
				return err
			}
			if *jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(page)
			}
			if len(page.Entries) == 0 {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), "No excluded images.")
				return err
			}
			var text strings.Builder
			table := tabwriter.NewWriter(&text, 0, 4, 2, ' ', 0)
			fmt.Fprintln(table, "ID\tProvider\tImage")
			for _, entry := range page.Entries {
				label := entry.Image
				if entry.SourceID != "" {
					label = entry.SourceID + "/" + entry.Image
				} else if entry.Title != "" {
					label = entry.Title
				}
				fmt.Fprintf(table, "%d\t%s\t%s\n", entry.ID, terminalText(entry.Provider), terminalText(label))
			}
			if err := table.Flush(); err != nil {
				return err
			}
			if page.NextBefore > 0 {
				fmt.Fprintf(&text, "More entries: panora exclusions ls --before %d", page.NextBefore)
				if *socket != "" {
					fmt.Fprintf(&text, " --socket %q", *socket)
				}
				text.WriteByte('\n')
			}
			_, err = io.WriteString(cmd.OutOrStdout(), text.String())
			return err
		},
	}
	cmd.Flags().Int64Var(&before, "before", 0, "list IDs older than this page cursor; 0 starts at newest")
	cmd.Flags().IntVar(&limit, "limit", playback.MaxExclusionsPageSize, "page size (1 through 10)")
	return cmd
}

func newExclusionsRemoveCommand(socket *string, jsonOutput *bool) *cobra.Command {
	return &cobra.Command{
		Use: "remove ID", Short: "Remove an exclusion so the image can play again", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil || id <= 0 {
				return &control.Error{Code: "invalid_request", Message: "exclusion ID must be a positive integer; use panora exclusions ls"}
			}
			path, err := control.SocketPath(*socket)
			if err != nil {
				return err
			}
			status, err := control.RestoreExclusion(cmd.Context(), path, id)
			if err != nil {
				return err
			}
			return writeActionResult(cmd, status, "restore-exclusion", *jsonOutput)
		},
	}
}
