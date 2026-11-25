package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/vadim82/panora/internal/control"
)

const quitInterface = "io.github.vadim82.panora.Window"
const quitPath = dbus.ObjectPath("/io/github/vadim82/panora/Window")

func quitBusName(socket string, managed bool) string {
	return quitInterface + "." + strings.ReplaceAll(windowIdentity(socket, managed), ".", "_")
}

type desktopQuit func()

func (quit desktopQuit) Quit() *dbus.Error {
	quit()
	return nil
}

func serveDesktopQuit(ctx context.Context, socket string, managed bool, quit func()) func() {
	if socket == "" {
		return nil
	}
	bus, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not connect the Panora quit endpoint:", err)
		return nil
	}
	err = bus.Export(desktopQuit(quit), quitPath, quitInterface)
	if err == nil {
		var reply dbus.RequestNameReply
		reply, err = bus.RequestName(quitBusName(socket, managed), dbus.NameFlagDoNotQueue)
		if err == nil && reply != dbus.RequestNameReplyPrimaryOwner {
			err = fmt.Errorf("another Panora window owns the quit endpoint")
		}
	}
	if err != nil {
		bus.Close()
		fmt.Fprintln(os.Stderr, "Could not register the Panora quit endpoint:", err)
		return nil
	}
	return func() { bus.Close() }
}

func desktopQuitOwners(ctx context.Context, bus *dbus.Conn, path string) ([]string, error) {
	var owners []string
	for _, managed := range []bool{true, false} {
		var owner string
		err := bus.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, quitBusName(path, managed)).Store(&owner)
		if dbusErrorIs(err, "NameHasNoOwner") {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("identify the open Panora window: %w", err)
		}
		owners = append(owners, owner)
	}
	return owners, nil
}

func quitDesktop(socket, instance string) error {
	if instance == "" || len(instance) > 128 {
		return fmt.Errorf("--quit requires an observed service instance ID via --instance")
	}
	path, err := control.SocketPath(socket)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	bus, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("connect to the Panora window session: %w", err)
	}
	defer bus.Close()
	result, err := quitDesktopInstance(ctx, bus, path, instance)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

func quitDesktopInstance(ctx context.Context, bus *dbus.Conn, path, instance string) (control.ShutdownResult, error) {
	owners, err := desktopQuitOwners(ctx, bus, path)
	if err != nil {
		return control.ShutdownResult{}, err
	}
	result, err := control.ShutdownInstance(ctx, path, instance)
	if err != nil {
		return result, err
	}
	if !result.Stopping || result.InstanceID != instance {
		return result, fmt.Errorf("unexpected shutdown acknowledgement")
	}
	var closeErr error
	for _, owner := range owners {
		// Use the captured unique names so a newly launched window cannot
		// take over a well-known name and receive the old window's Quit.
		err = bus.Object(owner, quitPath).CallWithContext(ctx, quitInterface+".Quit", dbus.FlagNoAutoStart).Err
		if dbusErrorIs(err, "NoReply") {
			// Closing the window can release its bus connection before the
			// reply arrives. Verify that the captured owner actually exited.
			var present bool
			check := bus.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.NameHasOwner", 0, owner).Store(&present)
			if check == nil && !present {
				continue
			}
		}
		if err != nil && !dbusErrorIs(err, "NameHasNoOwner") && !dbusErrorIs(err, "ServiceUnknown") {
			closeErr = errors.Join(closeErr, err)
		}
	}
	if closeErr != nil {
		return result, fmt.Errorf("close the Panora window: %w", closeErr)
	}
	return result, nil
}

func dbusErrorIs(err error, name string) bool {
	var busError dbus.Error
	return errors.As(err, &busError) && busError.Name == "org.freedesktop.DBus.Error."+name
}
