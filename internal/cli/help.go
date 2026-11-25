package cli

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func installHelp(root *cobra.Command) {
	root.AddGroup(
		&cobra.Group{ID: "playback", Title: "Playback controls:"},
		&cobra.Group{ID: "service", Title: "Foreground service:"},
		&cobra.Group{ID: "diagnostics", Title: "Diagnostics and configuration:"},
		&cobra.Group{ID: "advanced", Title: "Advanced and internal commands:"},
	)
	for _, cmd := range root.Commands() {
		switch cmd.Name() {
		case "next", "previous", "pause", "resume", "status", "exclusions", "history":
			cmd.GroupID = "playback"
		case "serve", "stop":
			cmd.GroupID = "service"
		case "provider", "debug", "shell":
			cmd.GroupID = "advanced"
		default:
			cmd.GroupID = "diagnostics"
		}
	}
	root.SetHelpCommandGroupID("diagnostics")
	root.SetCompletionCommandGroupID("advanced")
	root.Example = "  panora serve\n  panora next\n  panora status\n  panora config check"
	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		out := cmd.OutOrStdout()
		var plain bytes.Buffer
		cmd.SetOut(&plain)
		defaultHelp(cmd, args)
		cmd.SetOut(out)
		jsonOutput, _ := cmd.Flags().GetBool("json")
		for _, line := range strings.SplitAfter(plain.String(), "\n") {
			text := strings.TrimSuffix(line, "\n")
			if !jsonOutput && text != "" && !strings.HasPrefix(text, " ") && strings.HasSuffix(text, ":") {
				line = styled(out, "1;36", text) + "\n"
			}
			fmt.Fprint(out, line)
		}
	})
}
