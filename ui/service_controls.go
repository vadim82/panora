package main

import (
	"context"
	"fmt"
	"time"

	"github.com/vadim82/panora/internal/appservice"
)

type serviceManager interface {
	Status(context.Context) appservice.State
	Start(context.Context) (appservice.State, error)
	Stop(context.Context) (appservice.State, error)
	Restart(context.Context) (appservice.State, error)
	SetLogin(context.Context, bool) (appservice.State, error)
	RemoveStartup(context.Context) (appservice.State, error)
}

type ServiceResult struct {
	Service appservice.State `json:"service"`
	Error   string           `json:"error,omitempty"`
	Busy    bool             `json:"busy"`
}

func (c *Client) ServiceState() ServiceResult {
	if c.manager == nil {
		return ServiceResult{Error: c.startupMessage()}
	}
	ctx, cancel := context.WithTimeout(c.ctx, 6*time.Second)
	defer cancel()
	return ServiceResult{Service: c.manager.Status(ctx), Error: c.startupMessage(), Busy: c.lifecycleBusy.Load()}
}

func (c *Client) startupMessage() string {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	return c.startupError
}
func (c *Client) setStartupMessage(message string) {
	c.lifecycleMu.Lock()
	c.startupError = message
	c.lifecycleMu.Unlock()
}

// ServiceCommand has a fixed operation allowlist; webview input never supplies paths or executables.
func (c *Client) ServiceCommand(operation string) ServiceResult {
	if c.manager == nil {
		return ServiceResult{Error: "Service management requires the installed Panora app and its default connection."}
	}
	if !c.lifecycleAction.TryLock() {
		return ServiceResult{Error: "A service operation is still running. Wait and refresh its state."}
	}
	defer c.lifecycleAction.Unlock()
	c.lifecycleBusy.Store(true)
	defer c.lifecycleBusy.Store(false)
	ctx, cancel := context.WithTimeout(c.ctx, 20*time.Second)
	defer cancel()
	var state appservice.State
	var err error
	switch operation {
	case "start":
		state, err = c.manager.Start(ctx)
	case "restart":
		state, err = c.manager.Restart(ctx)
	case "stop":
		state, err = c.manager.Stop(ctx)
	case "login_on":
		state, err = c.manager.SetLogin(ctx, true)
	case "login_off":
		state, err = c.manager.SetLogin(ctx, false)
	case "remove_startup":
		state, err = c.manager.RemoveStartup(ctx)
	default:
		err = fmt.Errorf("unsupported service operation")
	}
	result := ServiceResult{Service: state}
	if err != nil {
		result.Error = err.Error()
		c.setStartupMessage(result.Error)
	} else {
		c.setStartupMessage("")
	}
	return result
}

func (c *Client) OpenServiceLogs() OpenResult {
	if c.manager == nil {
		return OpenResult{Error: "Managed service logs are unavailable for this connection."}
	}
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	directory := c.manager.Status(ctx).LogDirectory
	if directory == "" {
		return OpenResult{Error: "Managed service logs are unavailable as a folder for this connection."}
	}
	if err := c.opener(ctx, directory); err != nil {
		return OpenResult{Error: err.Error()}
	}
	return OpenResult{Opened: true}
}
