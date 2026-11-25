//go:build wails && !darwin

package main

import (
	"context"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func installNativeWindowReopen()                {}
func installNativeSettingsMenu(context.Context) {}

func notifyAboutLaunch(ctx context.Context) {
	wailsruntime.WindowExecJS(ctx, "window.dispatchEvent(new Event('panora:launch-about'))")
}
