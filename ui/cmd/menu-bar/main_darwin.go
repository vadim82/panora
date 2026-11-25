package main

/*
#cgo CFLAGS: -x objective-c -fblocks
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
#include "menu.h"
*/
import "C"

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/ui/internal/menubar"
)

func init() { runtime.LockOSThread() }

var actions = make(chan menubar.Action, 1)
var stops = make(chan struct{}, 1)

func main() {
	socket := flag.String("socket", "", "private Panora service socket")
	instance := flag.String("instance", "", "service instance that started this helper")
	flag.Parse()
	if err := run(*socket, *instance); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(socket, instance string) error {
	if socket == "" || C.panoraIsMenuBundle() == 0 {
		return fmt.Errorf("start Panora Menu from its dedicated helper bundle with a private service socket")
	}
	path, err := control.SocketPath(socket)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	session, err := menubar.Connect(ctx, path, instance)
	if err != nil {
		return err
	}
	defer session.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		session.Run(ctx, actions, stops, updateMenu)
		C.panoraQuitMenu()
	}()
	C.panoraRunMenu()
	cancel()
	<-done
	return nil
}

func updateMenu(state menubar.State) {
	message := C.CString(state.Message)
	defer C.free(unsafe.Pointer(message))
	flag := func(value bool) C.int {
		if value {
			return 1
		}
		return 0
	}
	C.panoraUpdateMenu(message, flag(state.Previous), flag(state.Pause), flag(state.Paused), flag(state.Next), flag(state.Stop))
}

//export panoraMenuAction
func panoraMenuAction(value C.int) {
	if value == C.PanoraStop {
		select {
		case stops <- struct{}{}:
		default:
		}
		return
	}
	action := map[C.int]menubar.Action{
		C.PanoraPrevious: menubar.Previous,
		C.PanoraPause:    menubar.Pause,
		C.PanoraResume:   menubar.Resume,
		C.PanoraNext:     menubar.Next,
	}[value]
	if action != "" {
		select {
		case actions <- action:
		default:
		}
	}
}
