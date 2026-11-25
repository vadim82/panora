package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/playback"
)

type diagnosticCheck struct {
	Name    string `json:"name"`
	State   string `json:"state"`
	Message string `json:"message"`
}

type doctorReport struct {
	Version           int               `json:"version"`
	ConfiguredBackend string            `json:"configured_backend,omitempty"`
	ResolvedBackend   string            `json:"resolved_backend,omitempty"`
	RunningBackend    string            `json:"running_requested_backend,omitempty"`
	Checks            []diagnosticCheck `json:"checks"`
	Error             *control.Error    `json:"error,omitempty"`
}

func newDoctorCommand() *cobra.Command {
	var configPath, socket string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use: "doctor", Short: "Check configuration, desktop readiness and service connectivity",
		Long:    "Run bounded, read-only setup checks. Nothing is started, repaired or applied.\nInvalid configuration or desktop readiness fails the check. A missing service is\na warning: foreground playback can be started separately with panora serve.\nSuccessful readiness probes do not verify rendered wallpaper pixels.",
		Example: "  panora doctor\n  panora doctor --config ./config.toml --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 8*time.Second)
			defer cancel()
			report := diagnose(ctx, configPath, socket)
			if jsonOutput {
				if err := json.NewEncoder(cmd.OutOrStdout()).Encode(report); err != nil {
					return err
				}
			} else {
				for _, check := range report.Checks {
					color := "32"
					if check.State == "failed" {
						color = "31"
					} else if check.State == "warning" {
						color = "33"
					}
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s: %s\n", styled(cmd.OutOrStdout(), color, strings.ToUpper(check.State)), check.Name, terminalText(check.Message)); err != nil {
						return err
					}
				}
			}
			if report.Error != nil {
				return &commandError{err: report.Error, reported: true}
			}
			return nil
		},
	}
	addConfigFlag(cmd, &configPath)
	cmd.Flags().StringVar(&socket, "socket", "", "private service control socket")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "print a versioned JSON diagnostic report")
	return cmd
}

func diagnose(ctx context.Context, configPath, socket string) doctorReport {
	report := doctorReport{Version: control.APIVersion}
	add := func(name, state, message string) {
		report.Checks = append(report.Checks, diagnosticCheck{Name: name, State: state, Message: message})
		if state == "failed" {
			report.Error = &control.Error{Code: "doctor_failed", Message: "setup checks failed; inspect the diagnostic report"}
		}
	}
	path, pathErr := playback.ConfigPath(configPath)
	var config playback.Config
	configErr := pathErr
	if pathErr == nil {
		config, _, configErr = playback.LoadConfig(path)
	}
	if configErr != nil {
		add("configuration", "failed", configErr.Error())
		add("desktop", "warning", "not probed because configuration is invalid")
	} else {
		add("configuration", "ok", path+" (file validation only)")
		report.ConfiguredBackend = config.Desktop.Backend
		selected, err := desktop.Select(ctx, config.Desktop.Backend)
		if err != nil {
			add("desktop", "failed", err.Error())
		} else {
			report.ResolvedBackend = selected.Name
			add("desktop", "ok", fmt.Sprintf("configured: %s; Backend: %s; %s; Scope: %s; Fit: %s", config.Desktop.Backend, selected.Name, selected.Readiness, selected.Scope, selected.Fit))
		}
	}
	// Service connectivity is independent of saved-file validation/readiness.
	serviceCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	socketPath, err := control.SocketPath(socket)
	var status control.Status
	if err == nil {
		status, err = control.Request(serviceCtx, socketPath, "")
	}
	if err != nil {
		add("service", "warning", fmt.Sprintf("%v; start panora serve when ready", err))
		return report
	}
	report.RunningBackend = status.Backend
	add("service", "ok", "reachable at "+socketPath+"; requested backend: "+status.Backend)
	if configErr == nil && config.Desktop.Backend != status.Backend {
		add("settings", "warning", "saved backend differs from the running service; restart serve to apply saved desktop settings")
	}
	return report
}
