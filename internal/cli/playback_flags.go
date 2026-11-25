package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/vadim82/panora/internal/playback"
	"github.com/vadim82/panora/internal/source"
	"github.com/vadim82/panora/internal/sources/wallhaven"
)

func addPlaybackFlags(cmd *cobra.Command, request *playback.Request) {
	var pluginDirectory string
	cmd.Flags().StringVar(&pluginDirectory, "local-plugin", "", "explicit local plugin directory with plugin.json (default: bundled child process)")
	cmd.PreRunE = func(cmd *cobra.Command, _ []string) error {
		request.Source = source.BundledLocal()
		request.ProviderEditors = nil
		request.Providers = map[string]source.Provider{"wallhaven": wallhaven.New()}
		if pluginDirectory != "" {
			client, err := source.Load(pluginDirectory)
			if err != nil {
				return err
			}
			request.Source = client
		}
		root, err := source.PluginDirectory()
		if err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Panora providers: %v\n", err)
			return nil
		}
		plugins, err := source.InstalledPlugins(root)
		if err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Panora providers: %v\n", err)
		}
		for _, entry := range plugins {
			editor := playback.ProviderEditor{ID: entry.ID, Name: entry.ID}
			if entry.Err != nil {
				editor.EditorError = "Check the installed plugin or reinstall it, then restart Panora’s service."
			} else {
				editor.Name = entry.Client.Manifest.Name
				request.Providers[entry.ID] = entry.Client
				manifest, err := source.DecodeEditorManifest(entry.Client.Manifest.Editor)
				if err != nil {
					editor.EditorError = err.Error()
				} else {
					editor.Editor = &manifest
				}
			}
			request.ProviderEditors = append(request.ProviderEditors, editor)
		}
		return nil
	}
	addConfigFlag(cmd, &request.ConfigPath)
	cmd.Flags().StringVar(&request.StatePath, "state-file", "", "playback state file (default: OS state directory/panora/state.db)")
	cmd.Flags().StringVar(&request.Options.Fit, "fit", "auto", "temporary display mode override: auto, contain, or crop")
	cmd.Flags().BoolVar(&request.Options.Upscale, "upscale", false, "temporary override allowing smaller images to be enlarged")
}
