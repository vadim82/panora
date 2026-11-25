package appservice

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/filelock"
)

const (
	systemdUnit     = "panora.service"
	systemdFragment = "/usr/lib/systemd/user/" + systemdUnit
	journalCommand  = "journalctl --user -u " + systemdUnit + " -b"
	stopCommand     = "systemctl --user stop " + systemdUnit
	startupStatus   = "Panora startup: "
)

var errCustomizedUnit = errors.New("the Panora user unit is overridden; inspect systemctl --user cat panora.service")

type SystemdManager struct {
	socket  string
	command func(context.Context, ...string) ([]byte, error)
	owns    func() bool
}

func NewSystemd(socket string) (*SystemdManager, error) {
	path, err := control.SocketPath("")
	if err != nil {
		return nil, err
	}
	if socket != path {
		return nil, fmt.Errorf("the installed service requires the default private control socket")
	}
	return &SystemdManager{
		socket: socket,
		command: func(ctx context.Context, args ...string) ([]byte, error) {
			return runCommand(ctx, "/usr/bin/systemctl", append([]string{"--user", "--no-pager"}, args...)...)
		},
		owns: func() bool {
			info, err := os.Lstat(systemdFragment)
			if err != nil {
				return false
			}
			stat, ok := info.Sys().(*syscall.Stat_t)
			return info.Mode().IsRegular() && ok && stat.Uid == 0 && info.Mode().Perm()&0022 == 0
		},
	}, nil
}

type systemdJob struct {
	active, result, status, login string
	pid                           int
	pending                       bool
}

func (job systemdJob) stopped() bool {
	return job.pid == 0 && !job.pending && (job.active == "inactive" || job.active == "failed")
}

func (m *SystemdManager) job(ctx context.Context) (systemdJob, error) {
	raw, err := m.command(ctx, "show", "--property=LoadState,FragmentPath,DropInPaths,ExecStart,ActiveState,MainPID,UnitFileState,Restart,Result,StatusText,Job", systemdUnit)
	if err != nil {
		return systemdJob{}, fmt.Errorf("Panora could not reach the systemd user manager: %w", err)
	}
	properties := make(map[string]string)
	for line := range strings.SplitSeq(string(raw), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			properties[key] = value
		}
	}
	fragment := properties["FragmentPath"]
	if properties["LoadState"] == "loaded" && (properties["DropInPaths"] != "" || (fragment != "" && fragment != systemdFragment)) {
		return systemdJob{}, errCustomizedUnit
	}
	if properties["LoadState"] != "loaded" || fragment != systemdFragment || properties["Restart"] != "no" ||
		!strings.HasPrefix(properties["ExecStart"], "{ path=/usr/bin/panora ;") || !m.owns() {
		return systemdJob{}, errors.New("the packaged Panora user unit is missing, masked or overridden; inspect systemctl --user cat panora.service")
	}
	pid, err := strconv.Atoi(properties["MainPID"])
	if err != nil || pid < 0 || properties["ActiveState"] == "" {
		return systemdJob{}, fmt.Errorf("the systemd user manager did not report Panora's process state")
	}
	switch properties["UnitFileState"] {
	case "enabled", "enabled-runtime", "disabled":
	default:
		return systemdJob{}, fmt.Errorf("the packaged Panora user unit has an unrecognized registration state")
	}
	return systemdJob{active: properties["ActiveState"], result: properties["Result"],
		status: properties["StatusText"], login: properties["UnitFileState"], pid: pid,
		pending: properties["Job"] != "" && properties["Job"] != "0"}, nil
}

type systemdSnapshot struct {
	state        State
	job          systemdJob
	peer         control.Peer
	socketAbsent bool
	ownsPeer     bool
}

func (m *SystemdManager) snapshot(ctx context.Context) systemdSnapshot {
	state := State{Platform: "linux", LoginState: "unknown", LogCommand: journalCommand}
	probeCtx, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	status, peer, socketErr := control.StatusWithPeer(probeCtx, m.socket)
	cancel()
	if socketErr == nil && status.InstanceID != "" {
		state.Running = true
		state.InstanceID = status.InstanceID
	}
	job, err := m.job(ctx)
	if err != nil {
		state.Error = err.Error()
		state.RemoveBlocked = true
		return systemdSnapshot{state: state, peer: peer}
	}
	state.Available = true
	state.StartAtLogin = job.login == "enabled"
	state.LoginState = "disabled"
	switch job.login {
	case "enabled":
		state.LoginState = "enabled_next_login"
	case "enabled-runtime":
		state.LoginState = "enabled_for_session"
	}
	ownsPeer := state.Running && peer.PID > 0 && peer.PID == job.pid
	state.Managed = ownsPeer && job.active == "active"
	state.StartBlocked = !state.Running && (peer.PID != 0 || (job.pid > 0 && job.active != "activating") ||
		(socketErr != nil && !errors.Is(socketErr, os.ErrNotExist) && !errors.Is(socketErr, syscall.ECONNREFUSED)))
	state.RemoveBlocked = state.StartBlocked || (!state.Managed && !job.stopped())
	switch {
	case state.StartBlocked && job.pid > 0:
		state.Error = apiUnavailable()
	case state.StartBlocked:
		state.Error = "The existing Panora socket is unavailable or incompatible. Check its owner before starting the installed service."
	case !state.Running && job.active == "failed":
		state.Error = job.failure()
	}
	return systemdSnapshot{state: state, job: job, peer: peer, socketAbsent: errors.Is(socketErr, os.ErrNotExist), ownsPeer: ownsPeer}
}

func apiUnavailable() string {
	return "The installed Panora process has no usable control API. Inspect " + journalCommand +
		", then run " + stopCommand + " before trying Start again."
}

func (job systemdJob) failure() string {
	if message, ok := strings.CutPrefix(job.status, startupStatus); ok && message != "" {
		return message + " See the Panora user journal."
	}
	if job.result == "dependency" {
		return "The graphical user session is unavailable. Open Panora after the Omarchy shell is ready."
	}
	return "Panora did not start. Check the Panora user journal for the startup error."
}

func (m *SystemdManager) Status(ctx context.Context) State {
	return m.snapshot(ctx).state
}

func (m *SystemdManager) lock(ctx context.Context) (func(), error) {
	return lockRuntime(ctx, filepath.Dir(m.socket))
}

func (m *SystemdManager) Start(ctx context.Context) (State, error) {
	release, err := m.lock(ctx)
	if err != nil {
		return State{}, err
	}
	defer release()
	return m.start(ctx, "")
}

func (m *SystemdManager) start(ctx context.Context, previous string) (State, error) {
	current := m.snapshot(ctx)
	if current.state.Running && (!current.ownsPeer || current.state.Managed) {
		if previous != "" && (!current.state.Managed || current.state.InstanceID == previous) {
			return current.state, fmt.Errorf("a different owner may have taken the control socket during restart")
		}
		return current.state, nil
	}
	if !current.state.Available || current.state.StartBlocked {
		return current.state, errors.New(current.state.Error)
	}
	if current.job.active != "activating" && !current.job.pending {
		graphical, err := m.command(ctx, "show", "--property=ActiveState", "--value", "graphical-session.target")
		if err != nil {
			return current.state, fmt.Errorf("could not verify the graphical user session: %w", err)
		}
		if strings.TrimSpace(string(graphical)) != "active" {
			return current.state, fmt.Errorf("graphical-session.target is inactive; open Panora after your Omarchy session is ready")
		}
		// The named job is not starting. A separate process that already has
		// the socket lock may still be opening its API.
		release, err := filelock.Try(m.socket + ".lock")
		if err != nil {
			return current.state, fmt.Errorf("could not acquire the private socket lock before Start: %w", err)
		}
		release()
		if _, err := m.command(ctx, "start", "--no-block", systemdUnit); err != nil {
			return current.state, fmt.Errorf("could not start Panora in the graphical user session: %w; inspect %s", err, journalCommand)
		}
	}
	wait, cancel := context.WithTimeout(ctx, 19*time.Second)
	defer cancel()
	for wait.Err() == nil {
		next := m.snapshot(wait)
		if wait.Err() != nil {
			break
		}
		current = next
		switch {
		case current.state.Managed && current.state.InstanceID != previous:
			return current.state, nil
		case current.state.Running && !current.ownsPeer:
			return current.state, fmt.Errorf("a separately started service took the control socket while Panora was starting")
		case current.state.StartBlocked || !current.state.Available:
			return current.state, fmt.Errorf("could not establish ownership of the installed service: %s", current.state.Error)
		case current.job.active == "failed" && !current.job.pending:
			return current.state, errors.New(current.job.failure())
		}
		if err := waitForJob(wait); err != nil {
			break
		}
	}
	if current.job.pid > 0 && !current.state.Running {
		return current.state, errors.New(apiUnavailable())
	}
	return current.state, fmt.Errorf("Panora did not become ready in the graphical user session: %w; inspect %s", wait.Err(), journalCommand)
}

func (m *SystemdManager) Stop(ctx context.Context) (State, error) {
	release, err := m.lock(ctx)
	if err != nil {
		return State{}, err
	}
	defer release()
	return m.stop(ctx, m.snapshot(ctx))
}

func (m *SystemdManager) stop(ctx context.Context, current systemdSnapshot) (State, error) {
	if !current.state.Managed {
		if current.state.StartBlocked && current.job.pid > 0 {
			return current.state, errors.New(current.state.Error)
		}
		return current.state, fmt.Errorf("Panora cannot verify this socket belongs to the installed service")
	}
	// Bind the actual shutdown request to the verified peer. A replacement
	// process never receives the mutation even if it takes the same socket.
	request, cancel := context.WithTimeout(ctx, 2*time.Second)
	shutdown, requestErr := control.ShutdownPeer(request, m.socket, current.peer)
	cancel()
	if requestErr == nil && (!shutdown.Stopping || shutdown.InstanceID != current.state.InstanceID) {
		requestErr = fmt.Errorf("unexpected shutdown acknowledgement")
	}
	for {
		next := m.snapshot(ctx)
		if (next.peer.PID != 0 && next.peer != current.peer) || (next.state.Running && next.state.InstanceID != current.state.InstanceID) {
			return next.state, fmt.Errorf("a different Panora service appeared while waiting for shutdown")
		}
		if next.socketAbsent && next.state.Available && next.job.stopped() {
			release, err := filelock.Try(m.socket + ".lock")
			if err == nil {
				release()
				return next.state, nil
			}
		}
		if err := waitForJob(ctx); err != nil {
			return next.state, fmt.Errorf("could not verify that Panora stopped: %w", errors.Join(err, requestErr))
		}
	}
}

func (m *SystemdManager) Restart(ctx context.Context) (State, error) {
	release, err := m.lock(ctx)
	if err != nil {
		return State{}, err
	}
	defer release()
	current := m.snapshot(ctx)
	if state, err := m.stop(ctx, current); err != nil {
		return state, err
	}
	return m.start(ctx, current.state.InstanceID)
}

func (m *SystemdManager) SetLogin(ctx context.Context, enabled bool) (State, error) {
	release, err := m.lock(ctx)
	if err != nil {
		return State{}, err
	}
	defer release()
	return m.setLogin(ctx, enabled)
}

func (m *SystemdManager) setLogin(ctx context.Context, enabled bool) (State, error) {
	if _, err := m.job(ctx); err != nil {
		return m.Status(ctx), err
	}
	action := "disable"
	if enabled {
		action = "enable"
	}
	if _, err := m.command(ctx, action, systemdUnit); err != nil {
		return m.Status(ctx), fmt.Errorf("could not update Panora login registration: %w", err)
	}
	state := m.Status(ctx)
	if !state.Available || state.StartAtLogin != enabled || (!enabled && state.LoginState != "disabled") {
		return state, fmt.Errorf("could not verify the updated Panora login registration")
	}
	return state, nil
}

func (m *SystemdManager) RemoveStartup(ctx context.Context) (State, error) {
	release, err := m.lock(ctx)
	if err != nil {
		return State{}, err
	}
	defer release()
	current := m.snapshot(ctx)
	if !current.state.Available || current.state.StartBlocked {
		return current.state, errors.New(current.state.Error)
	}
	if current.state.RemoveBlocked {
		return current.state, fmt.Errorf("Panora cannot verify that the installed service is either stopped or owns the control socket")
	}
	if current.state.Managed {
		if state, err := m.stop(ctx, current); err != nil {
			return state, err
		}
	}
	return m.setLogin(ctx, false)
}

func waitForJob(ctx context.Context) error {
	timer := time.NewTimer(120 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
