package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/vadim82/panora/internal/appservice"
	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/playback"
	"github.com/vadim82/panora/internal/service"
)

func newServeCommand(resolve func(context.Context) (desktop.WallpaperBackend, error)) *cobra.Command {
	var request playback.Request
	var socket string
	var interval time.Duration
	var paused bool
	var pauseForSetup bool
	var omarchySession bool
	var logPath string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the wallpaper service in the foreground",
		Long:  "Own rotation and accepted-request history until Ctrl-C or SIGTERM.\nAn absent default config is created automatically; with no enabled inputs, wait for setup.\nExisting files are never reset; an explicit --config file must already exist.\nControl it from another terminal with status, next, previous, pause, and resume.\nStandalone use does not install a login service or desktop settings. Pause and history survive restart. The interval comes from TOML unless overridden.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) (runErr error) {
			ctx := cmd.Context()
			startupFailure := "Panora could not initialize its private control socket or saved playback state."
			var hooks service.Hooks
			started := false
			if omarchySession {
				if runtime.GOOS != "linux" {
					return fmt.Errorf("--omarchy-session requires Linux")
				}
				defer func() {
					if !started && runErr != nil && cmd.Context().Err() == nil && !errors.Is(runErr, context.Canceled) {
						_ = notifySystemd("STATUS=Panora startup: " + startupFailure)
					}
				}()
				serviceCtx, release, err := appservice.ArchPackageContext(ctx)
				if err != nil {
					startupFailure = "Panora could not verify its package installation. Check the user journal."
					return err
				}
				defer release()
				ctx = serviceCtx
				if err := waitForOmarchySession(ctx, 8*time.Second); err != nil {
					startupFailure = err.Error()
					return err
				}
			}
			if logPath != "" {
				log, err := service.OpenLog(logPath)
				if err != nil {
					return fmt.Errorf("open service log: %w", err)
				}
				defer log.Close()
				cmd.SetOut(io.MultiWriter(cmd.OutOrStdout(), log))
				cmd.SetErr(io.MultiWriter(cmd.ErrOrStderr(), log))
				defer func() {
					if runErr != nil {
						_, _ = fmt.Fprintln(log, "Service could not run. Existing settings were kept; check configuration and desktop readiness.")
					}
				}()
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			path, created, err := service.PrepareConfig(request.ConfigPath, cmd.OutOrStdout(), omarchySession)
			if err != nil {
				startupFailure = "Panora could not read its saved settings. Check config.toml and try Start again."
				return &commandError{code: "config_invalid", err: err}
			}
			request.ConfigPath = path
			config, _, err := playback.LoadConfig(request.ConfigPath)
			if err != nil {
				startupFailure = "Panora could not read its saved settings. Check config.toml and try Start again."
				return &commandError{code: "config_invalid", err: err}
			}
			request.Options = configuredDisplayOptions(cmd, config.Desktop, request.Options)
			resolver := resolve
			if resolver == nil {
				if runtime.GOOS == "darwin" {
					hooks.WatchDesktop = desktop.WatchMacOSSpaces
				}
				request.InspectCapabilities = desktop.InspectCapabilities
				request.ResolveBackend = func(ctx context.Context, name string) (desktop.WallpaperBackend, error) {
					selected, err := desktop.Select(ctx, name)
					return selected.Backend, err
				}
				resolver = func(ctx context.Context) (desktop.WallpaperBackend, error) {
					return request.ResolveBackend(ctx, config.Desktop.Backend)
				}
			}
			path, err = control.SocketPath(socket)
			if err != nil {
				return err
			}
			if runtime.GOOS == "darwin" && resolve == nil {
				defaultSocket, _ := control.SocketPath("")
				if pauseForSetup || path == defaultSocket {
					hooks.RunCompanion = installedMacOSMenu(path, cmd.ErrOrStderr())
				}
				if pauseForSetup {
					watchCtx, cancel := context.WithCancel(ctx)
					defer cancel()
					hooks.PackageReplacement = appservice.WatchHomebrewUpgrade(watchCtx, path)
				}
			}
			if omarchySession {
				barDone := make(chan struct{})
				defer func() {
					if started {
						<-barDone
					}
				}()
				hooks.Ready = func(ctx context.Context) error {
					if err := announceSystemdReady(ctx, path); err != nil {
						startupFailure = "Panora could not confirm the control API and report readiness to the systemd user manager."
						return err
					}
					started = true
					go func() {
						defer close(barDone)
						barCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
						defer cancel()
						if err := packagedOmarchyBar(path).complete(barCtx); err != nil && ctx.Err() == nil {
							fmt.Fprintf(cmd.ErrOrStderr(), "Omarchy bar setup failed: %v. Check whether Omarchy Shell is running, then retry with systemctl --user restart panora.service.\n", err)
						}
					}()
					return nil
				}
			}
			options := playback.ServiceOptions{
				Request:             request,
				Interval:            interval,
				IntervalChanged:     cmd.Flags().Changed("interval"),
				Paused:              paused || (pauseForSetup && !config.HasEnabledInputs()),
				Backend:             config.Desktop.Backend,
				FreshOmarchyProfile: omarchySession && created,
			}
			err = service.Run(ctx, options, resolver, path, cmd.OutOrStdout(), cmd.ErrOrStderr(), hooks)
			var replacement *service.PackageReplacement
			if errors.As(err, &replacement) && ctx.Err() == nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "Homebrew linked a new Panora release. Restarting the managed service.")
				if err := appservice.ExecHomebrewService(replacement.Executable); err != nil {
					return fmt.Errorf("start the linked Homebrew service: %w", err)
				}
			}
			return err
		},
	}
	addPlaybackFlags(cmd, &request)
	cmd.Flags().StringVar(&socket, "socket", "", "private control socket (default: platform runtime directory/panora/control.sock)")
	cmd.Flags().DurationVar(&interval, "interval", 30*time.Minute, "rotation interval for this run; omitted uses TOML playback.interval or 30m")
	cmd.Flags().BoolVar(&paused, "paused", false, "start with automatic rotation paused; manual next/previous still work")
	cmd.Flags().BoolVar(&pauseForSetup, "pause-for-setup", false, "start paused when no inputs are enabled; preserve saved pause for configured libraries")
	cmd.Flags().BoolVar(&omarchySession, "omarchy-session", false, "require the Omarchy graphical session and start fresh profiles with the active theme")
	cmd.Flags().StringVar(&logPath, "log-file", "", "also write bounded service output to this absolute private log path (two files, 1 MiB each)")
	return cmd
}

func newControlCommand(name, description string) *cobra.Command {
	var socket, instance string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use: name, Short: description, Args: cobra.NoArgs, Annotations: map[string]string{"control": "true"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			action := name
			if action == "status" {
				action = ""
			}
			if cmd.Flags().Changed("instance") && instance == "" {
				return &control.Error{Code: "invalid_request", Message: "--instance requires a nonempty service instance ID"}
			}
			return runControlCommand(cmd, socket, action, instance, jsonOutput)
		},
	}
	cmd.Flags().StringVar(&socket, "socket", "", "private control socket (default: platform runtime directory/panora/control.sock)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "print the versioned service response as JSON")
	if name == "status" {
		cmd.Flags().Bool("verbose", false, "include provider refresh, retry and history details")
	} else {
		cmd.Flags().StringVar(&instance, "instance", "", "send the action only to the given service instance")
	}
	return cmd
}

func runControlCommand(cmd *cobra.Command, socket, action, instance string, jsonOutput bool) error {
	path, err := control.SocketPath(socket)
	if err != nil {
		return err
	}
	status, err := control.RequestInstance(cmd.Context(), path, action, instance)
	if err != nil {
		return err
	}
	if action == "" {
		return writeControlStatus(cmd, status, jsonOutput)
	}
	return writeActionResult(cmd, status, action, jsonOutput)
}
