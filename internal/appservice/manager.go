//go:build darwin || linux

// Package appservice manages installed user-session process registration.
// It owns no image selection, playback timer, TOML edits or catalog writes.
package appservice

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/filelock"
)

const DefaultLabel = "org.panora.service"

type Options struct {
	Executable, Home, Socket, Label string
}

type State struct {
	Platform      string `json:"platform,omitempty"`
	Available     bool   `json:"available"`
	Running       bool   `json:"running"`
	Managed       bool   `json:"managed"`
	StartBlocked  bool   `json:"start_blocked,omitempty"`
	RemoveBlocked bool   `json:"remove_blocked,omitempty"`
	StartAtLogin  bool   `json:"start_at_login"`
	LoginState    string `json:"login_state"`
	NeedsRestart  bool   `json:"needs_restart"`
	InstanceID    string `json:"instance_id,omitempty"`
	LogDirectory  string `json:"log_directory,omitempty"`
	LogCommand    string `json:"log_command,omitempty"`
	Error         string `json:"error,omitempty"`
}

type Manager struct {
	options                                     Options
	runtime, registration, target, logDirectory string
	command                                     func(context.Context, ...string) ([]byte, error)
	probe                                       func(context.Context) (control.Status, error)
	shutdown                                    func(context.Context) (control.ShutdownResult, error)
}

func New(options Options) (*Manager, error) {
	if options.Label == "" {
		options.Label = DefaultLabel
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]{1,127}$`).MatchString(options.Label) {
		return nil, fmt.Errorf("invalid service label")
	}
	for _, path := range []string{options.Executable, options.Home, options.Socket} {
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("application service paths must be absolute")
		}
	}
	info, err := os.Stat(options.Executable)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return nil, fmt.Errorf("bundled Panora service is missing or not executable")
	}
	runtime := filepath.Dir(options.Socket)
	manager := &Manager{options: options, runtime: runtime,
		registration: filepath.Join(options.Home, "Library", "LaunchAgents", options.Label+".plist"),
		target:       "gui/" + strconv.Itoa(os.Getuid()) + "/" + options.Label,
		logDirectory: filepath.Join(options.Home, "Library", "Logs", "Panora"), command: launchctl}
	manager.probe = func(ctx context.Context) (control.Status, error) {
		child, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		return control.Request(child, options.Socket, "")
	}
	manager.shutdown = func(ctx context.Context) (control.ShutdownResult, error) {
		return control.Shutdown(ctx, options.Socket)
	}
	return manager, nil
}

func (m *Manager) Status(ctx context.Context) State {
	state := State{Platform: "darwin", Available: true, LoginState: "disabled", LogDirectory: m.logDirectory}
	status, err := m.probe(ctx)
	if err == nil {
		state.Running = true
		state.InstanceID = status.InstanceID
	}
	current, found, err := m.readOwned(m.registration)
	if err != nil {
		state.Error = err.Error()
		state.LoginState = "unknown"
		return state
	}
	state.StartAtLogin = found && current.RunAtLoad
	state.NeedsRestart = found && !slices.Equal(current.Arguments, m.arguments())
	if state.StartAtLogin {
		state.LoginState = "enabled_next_login"
	}
	loaded, owned, err := m.job(ctx)
	if err != nil {
		state.Error = err.Error()
		return state
	}
	state.Managed = loaded && owned
	if state.StartAtLogin {
		raw, err := m.command(ctx, "print-disabled", m.domain())
		if err != nil {
			state.LoginState = "unknown"
			state.Error = "Login startup registration could not be checked."
			return state
		}
		disabled := regexp.MustCompile(`"` + regexp.QuoteMeta(m.options.Label) + `"\s*=>\s*true`).Match(raw)
		if disabled {
			state.LoginState = "disabled_by_system"
		}
	}
	return state
}

func (m *Manager) Start(ctx context.Context) (State, error) {
	release, err := m.lock(ctx)
	if err != nil {
		return State{}, err
	}
	defer release()
	if status, err := m.probe(ctx); err == nil {
		migrating, err := m.stopLegacyHomebrew(ctx)
		if err != nil {
			return m.Status(ctx), err
		}
		if !migrating {
			state := m.Status(ctx)
			state.InstanceID = status.InstanceID
			return state, nil
		}
	}
	// A socket whose owner is still alive must not be replaced after an API error.
	if _, err := os.Lstat(m.options.Socket); err == nil {
		releaseSocket, lockErr := filelock.Try(m.options.Socket + ".lock")
		if lockErr != nil {
			return m.Status(ctx), fmt.Errorf("an existing service is unavailable or incompatible; inspect it before starting another: %w", lockErr)
		}
		releaseSocket()
	} else if !os.IsNotExist(err) {
		return State{}, err
	}
	current, atLogin, err := m.readOwned(m.registration)
	if err != nil {
		return State{}, err
	}
	loaded, owned, err := m.job(ctx)
	if err != nil {
		return State{}, err
	}
	if loaded {
		// The socket is down. Remove only the named registration whose
		// program and saved configuration match; never kill an arbitrary process.
		if !owned {
			return State{}, fmt.Errorf("existing launch agent ownership cannot be verified")
		}
		if _, err = m.command(ctx, "bootout", m.target); err != nil {
			return State{}, err
		}
	}
	if err := os.MkdirAll(m.logDirectory, 0700); err != nil {
		return State{}, err
	}
	config := agent{Label: m.options.Label, Arguments: m.arguments(), Home: m.options.Home, RunAtLoad: atLogin && current.RunAtLoad}
	if err := os.MkdirAll(filepath.Dir(m.registration), 0700); err != nil {
		return State{}, err
	}
	if err := m.write(m.registration, config.marshal()); err != nil {
		return State{}, err
	}
	if _, err = m.ensureEnabled(ctx); err != nil {
		return State{}, err
	}
	if _, err = m.command(ctx, "bootstrap", m.domain(), m.registration); err != nil {
		return State{}, err
	}
	if _, err = m.command(ctx, "kickstart", m.target); err != nil {
		return State{}, err
	}
	if err := m.wait(ctx, true, ""); err != nil {
		return m.Status(ctx), fmt.Errorf("Panora did not become ready; check %s: %w", filepath.Join(m.logDirectory, "service.log"), err)
	}
	return m.Status(ctx), nil
}

// Stop uses the private shutdown API, then waits for both socket and owner lock.
// KeepAlive is false; neither launchd nor polling restarts an intentional stop.
func (m *Manager) Stop(ctx context.Context) (State, error) {
	release, err := m.lock(ctx)
	if err != nil {
		return State{}, err
	}
	defer release()
	status, err := m.probe(ctx)
	if err != nil {
		return m.Status(ctx), fmt.Errorf("service state could not be verified; Stop was not replayed: %w", err)
	}
	_, err = m.shutdown(ctx)
	if err != nil {
		return m.Status(ctx), err
	}
	if err := m.wait(ctx, false, status.InstanceID); err != nil {
		return m.Status(ctx), err
	}
	return m.Status(ctx), nil
}

func (m *Manager) Restart(ctx context.Context) (State, error) {
	// Stop and Start each hold the same cross-process lock. Start rechecks the
	// socket, so a concurrent explicit Start is reused rather than duplicated.
	state := m.Status(ctx)
	if state.Running {
		if !state.Managed {
			return state, fmt.Errorf("this service was started separately; stop it before starting the bundled service")
		}
		if _, err := m.Stop(ctx); err != nil {
			return m.Status(ctx), err
		}
	}
	return m.Start(ctx)
}

func (m *Manager) SetLogin(ctx context.Context, enabled bool) (State, error) {
	release, err := m.lock(ctx)
	if err != nil {
		return State{}, err
	}
	defer release()
	config, found, err := m.readOwned(m.registration)
	if err != nil {
		return State{}, err
	}
	if !found {
		config = agent{Label: m.options.Label, Arguments: m.arguments(), Home: m.options.Home}
	}
	config.RunAtLoad = enabled
	if err := os.MkdirAll(filepath.Dir(m.registration), 0700); err != nil {
		return State{}, err
	}
	if err := m.write(m.registration, config.marshal()); err != nil {
		return State{}, err
	}
	if enabled {
		if _, err := m.ensureEnabled(ctx); err != nil {
			return m.Status(ctx), err
		}
	}
	return m.Status(ctx), nil
}

// RemoveStartup is explicit cleanup for moving/removing the app. It leaves all
// playback data intact and never unloads another application's launch agent.
func (m *Manager) RemoveStartup(ctx context.Context) (State, error) {
	release, err := m.lock(ctx)
	if err != nil {
		return State{}, err
	}
	defer release()
	_, registered, err := m.readOwned(m.registration)
	if err != nil {
		return State{}, err
	}
	loaded, owned, err := m.job(ctx)
	if err != nil {
		return State{}, err
	}
	if loaded && !owned {
		return State{}, fmt.Errorf("launch agent ownership cannot be verified")
	}
	if loaded {
		if _, err := m.command(ctx, "bootout", m.target); err != nil {
			return State{}, err
		}
	}
	if registered {
		if err := os.Remove(m.registration); err != nil {
			return State{}, err
		}
		if err := syncDirectory(filepath.Dir(m.registration)); err != nil {
			return m.Status(ctx), err
		}
	}
	return m.Status(ctx), nil
}

func (m *Manager) arguments() []string {
	args := []string{m.options.Executable, "serve", "--socket", m.options.Socket, "--pause-for-setup", "--log-file", filepath.Join(m.logDirectory, "service.log")}
	return args
}
func (m *Manager) domain() string { return filepath.Dir(m.target) }
func (m *Manager) ensureEnabled(ctx context.Context) ([]byte, error) {
	raw, err := m.command(ctx, "print-disabled", m.domain())
	if err != nil {
		return nil, err
	}
	disabled := regexp.MustCompile(`"` + regexp.QuoteMeta(m.options.Label) + `"\s*=>\s*true`).Match(raw)
	if disabled {
		return m.command(ctx, "enable", m.target)
	}
	return nil, nil
}

// job uses only the named job's printed source/program to verify ownership.
// Unknown output is treated as unowned; it never authorizes bootout.
func (m *Manager) job(ctx context.Context) (bool, bool, error) {
	loaded, owned, _, err := m.jobProcess(ctx)
	return loaded, owned, err
}

func (m *Manager) jobProcess(ctx context.Context) (bool, bool, int, error) {
	raw, err := m.command(ctx, "print", m.target)
	if err != nil {
		var exit interface{ ExitCode() int }
		if errors.As(err, &exit) && (exit.ExitCode() == 113 || exit.ExitCode() == 3) {
			return false, false, 0, nil
		}
		return false, false, 0, err
	}
	var path, program string
	var pid int
	for line := range strings.SplitSeq(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if value, ok := strings.CutPrefix(line, "path = "); ok {
			path = value
		}
		if value, ok := strings.CutPrefix(line, "program = "); ok {
			program = value
		}
		if value, ok := strings.CutPrefix(line, "pid = "); ok {
			pid, _ = strconv.Atoi(value)
		}
	}
	switch {
	case samePath(path, m.registration):
		path = m.registration
	default:
		return true, false, 0, nil
	}
	config, found, err := m.readOwned(path)
	if err != nil {
		return true, false, 0, err
	}
	return true, found && samePath(config.Arguments[0], program), pid, nil
}

func samePath(first, second string) bool {
	if first == second {
		return true
	}
	resolvedFirst, err := filepath.EvalSymlinks(first)
	if err != nil {
		return false
	}
	resolvedSecond, err := filepath.EvalSymlinks(second)
	return err == nil && resolvedFirst == resolvedSecond
}

func (m *Manager) wait(ctx context.Context, ready bool, previous string) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, err := m.probe(ctx)
		if ready && err == nil {
			return nil
		}
		if !ready {
			if err == nil && status.InstanceID != previous {
				return fmt.Errorf("a different service appeared while waiting for shutdown; no further action was taken")
			}
			if err != nil {
				release, lockErr := filelock.Try(m.options.Socket + ".lock")
				if lockErr == nil {
					release()
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func (m *Manager) readOwned(path string) (agent, bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return agent{}, false, nil
	}
	if err != nil {
		return agent{}, false, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || stat.Uid != uint32(os.Getuid()) || info.Mode().Perm()&0022 != 0 {
		return agent{}, false, fmt.Errorf("refusing unsafe startup registration %s", path)
	}
	config, err := readAgent(path)
	if err != nil {
		return agent{}, false, err
	}
	if config.Label != m.options.Label || config.Home != m.options.Home || len(config.Arguments) < 2 || !slices.Equal(config.Arguments[1:], m.arguments()[1:]) || !filepath.IsAbs(config.Arguments[0]) || filepath.Base(config.Arguments[0]) != "panora" {
		return agent{}, false, fmt.Errorf("refusing startup registration not owned by Panora: %s", path)
	}
	return config, true, nil
}
func (m *Manager) lock(ctx context.Context) (func(), error) {
	return lockRuntime(ctx, m.runtime)
}

func lockRuntime(ctx context.Context, runtime string) (func(), error) {
	if err := os.MkdirAll(runtime, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(runtime)
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || !ok || stat.Uid != uint32(os.Getuid()) || info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("service runtime directory must be private and owned by you")
	}
	return filelock.Acquire(ctx, filepath.Join(runtime, "appservice.lock"))
}
func (m *Manager) write(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".panora-agent-")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(temporary, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
