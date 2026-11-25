package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vadim82/panora/internal/control"
)

type commandError struct {
	code     string
	err      error
	reported bool
}

func (e *commandError) Error() string { return e.err.Error() }
func (e *commandError) Unwrap() error { return e.err }

// Execute owns error output, including failures before a command's RunE runs.
// Callers use its error for the exit status and must not print it a second time.
func Execute(ctx context.Context, args []string, out, errOut io.Writer) error {
	root := NewRootCommand()
	root.SetArgs(args)
	root.SetOut(out)
	root.SetErr(errOut)
	cmd, err := root.ExecuteContextC(ctx)
	if err == nil || errors.Is(err, context.Canceled) {
		return err
	}
	code := "command_failed"
	var coded *commandError
	if errors.As(err, &coded) && coded.reported {
		return err
	}
	var apiError *control.Error
	switch {
	case errors.As(err, &coded):
		code = coded.code
	case errors.As(err, &apiError):
		code = apiError.Code
	case isControlCommand(cmd):
		code = "control_request_failed"
	}
	if jsonRequested(cmd, args) {
		if outputErr := json.NewEncoder(out).Encode(struct {
			Version int            `json:"version"`
			Error   *control.Error `json:"error"`
		}{control.APIVersion, &control.Error{Code: code, Message: err.Error()}}); outputErr != nil {
			return outputErr
		}
	} else {
		if _, outputErr := fmt.Fprintf(errOut, "%s %s\n", styled(errOut, "31", "Error:"), terminalText(err.Error())); outputErr != nil {
			return outputErr
		}
	}
	return err
}

// Look past an early parser error, but never mistake a flag operand or a literal
// after -- for an output-mode request. Only commands offering JSON honor it.
func jsonRequested(cmd *cobra.Command, args []string) bool {
	if cmd == nil || cmd.Flag("json") == nil {
		return false
	}
	enabled := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if arg == "--json" {
			enabled = true
			continue
		}
		if value, found := strings.CutPrefix(arg, "--json="); found {
			parsed, err := strconv.ParseBool(value)
			enabled = err != nil || parsed
			continue
		}
		if strings.HasPrefix(arg, "--") && !strings.Contains(arg, "=") {
			if flag := cmd.Flag(strings.TrimPrefix(arg, "--")); flag != nil && flag.NoOptDefVal == "" {
				i++
			}
		}
	}
	return enabled
}

func installArgumentErrors(cmd *cobra.Command) {
	if validate := cmd.Args; validate != nil {
		cmd.Args = func(cmd *cobra.Command, args []string) error {
			if err := validate(cmd, args); err != nil {
				return argumentError(cmd, err)
			}
			return nil
		}
	}
	cmd.SetFlagErrorFunc(argumentError)
	for _, child := range cmd.Commands() {
		installArgumentErrors(child)
	}
}

func argumentError(cmd *cobra.Command, err error) error {
	return &commandError{code: "invalid_arguments", err: fmt.Errorf("%w; run %s --help", err, cmd.CommandPath())}
}

func isControlCommand(cmd *cobra.Command) bool {
	for current := cmd; current != nil; current = current.Parent() {
		if current.Annotations["control"] == "true" {
			return true
		}
	}
	return false
}
