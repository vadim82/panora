package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/playback"
)

func newHistoryCommand() *cobra.Command {
	var socket string
	var jsonOutput bool
	cmd := &cobra.Command{Use: "history", Short: "List accepted wallpaper requests or replay an entry", Annotations: map[string]string{"control": "true"}, Example: "  panora history ls\n  panora history replay HISTORY_ID"}
	cmd.PersistentFlags().StringVar(&socket, "socket", "", "private service control socket")
	cmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "print the versioned service response as JSON")
	var before string
	var limit int
	list := &cobra.Command{Use: "ls", Short: "List newest history entries with stable replay IDs", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		path, err := control.SocketPath(socket)
		if err != nil {
			return err
		}
		page, err := control.ListHistory(cmd.Context(), path, before, limit)
		if err != nil {
			return err
		}
		if jsonOutput {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(page)
		}
		if len(page.Entries) == 0 {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "No history entries.")
			return err
		}
		var text strings.Builder
		table := tabwriter.NewWriter(&text, 0, 4, 2, ' ', 0)
		fmt.Fprintln(table, "ID\tCurrent\tProvider\tImage\tAvailability")
		for _, entry := range page.Entries {
			current := ""
			if entry.Current {
				current = "*"
			}
			label := entry.Image
			if entry.Attribution != nil && entry.Attribution.Title != "" {
				label = entry.Attribution.Title
			}
			availability := entry.Availability + " (" + entry.Reason + ")"
			fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n", terminalText(entry.ID), current, terminalText(entry.Provider), terminalText(label), terminalText(availability))
		}
		if err := table.Flush(); err != nil {
			return err
		}
		if page.NextBefore != "" {
			fmt.Fprintf(&text, "More entries: panora history ls --before %q", page.NextBefore)
			if socket != "" {
				fmt.Fprintf(&text, " --socket %q", socket)
			}
			text.WriteByte('\n')
		}
		_, err = fmt.Fprint(cmd.OutOrStdout(), text.String())
		return err
	}}
	list.Flags().StringVar(&before, "before", "", "list entries older than this history ID")
	list.Flags().IntVar(&limit, "limit", playback.MaxHistoryPageSize, "page size (1 through 10)")
	// Bare history is the same first-page list; paging flags belong to ls.
	cmd.RunE, cmd.Args = list.RunE, cobra.NoArgs
	replay := &cobra.Command{Use: "replay ID", Short: "Replay a retained history entry using current image rules", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		path, err := control.SocketPath(socket)
		if err != nil {
			return err
		}
		status, err := control.ReplayHistory(cmd.Context(), path, args[0])
		if err != nil {
			return err
		}
		return writeActionResult(cmd, status, "replay", jsonOutput)
	}}
	cmd.AddCommand(list, replay)
	return cmd
}
