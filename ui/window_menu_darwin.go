//go:build wails && darwin

package main

/*
#cgo CFLAGS: -x objective-c -fblocks
#cgo LDFLAGS: -framework Cocoa -framework WebKit
void panoraInstallReopenHandler(void);
void panoraInstallSettingsMenu(void);
void panoraNotifyAboutLaunch(void);
*/
import "C"

import "context"

func installNativeWindowReopen()                { C.panoraInstallReopenHandler() }
func installNativeSettingsMenu(context.Context) { C.panoraInstallSettingsMenu() }
func notifyAboutLaunch(context.Context)         { C.panoraNotifyAboutLaunch() }
