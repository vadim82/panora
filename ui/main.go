package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/vadim82/panora/internal/control"
)

func main() {
	socket := flag.String("socket", "", "existing Panora private control socket")
	about := flag.Bool("about", false, "open About Panora")
	version := flag.Bool("version", false, "print the build identity")
	quit := flag.Bool("quit", false, "stop the observed service and close its open Panora windows (Linux)")
	instance := flag.String("instance", "", "observed service instance for --quit")
	flag.Parse()
	if *version {
		fmt.Println(readBuildIdentity())
		return
	}
	if *quit {
		if err := quitDesktop(*socket, *instance); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	socketExplicit := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "socket" {
			socketExplicit = true
		}
	})
	path, err := control.SocketPath(*socket)
	if err != nil && !socketExplicit && (*about || installedLinuxClient()) {
		// Display the runtime failure without selecting a substitute socket.
		err = runDesktop("", nil, fmt.Sprintf("%s. Reopen Panora from a ready Omarchy session.", err), *about)
	} else if err == nil {
		manager, managerErr := managedService(path, socketExplicit)
		startupError := ""
		if managerErr != nil {
			startupError = managerErr.Error()
		}
		err = runDesktop(path, manager, startupError, *about)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func installedLinuxClient() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	executable, err := os.Executable()
	return err == nil && executable == "/usr/bin/panora-ui"
}
