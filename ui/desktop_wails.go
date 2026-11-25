//go:build wails

package main

import (
	"context"
	"embed"
	"io/fs"
	"runtime"
	"slices"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var frontend embed.FS

func runDesktop(socket string, manager serviceManager, startupError string, about bool) error {
	assets, err := fs.Sub(frontend, "frontend/dist")
	if err != nil {
		return err
	}
	client := &Client{socket: socket, opener: openDesktopLocation, configOpener: openDesktopConfiguration, manager: manager, startupError: startupError}
	client.aboutRequested.Store(about)
	client.picker = func(ctx context.Context) (string, error) {
		return wailsruntime.OpenDirectoryDialog(ctx, wailsruntime.OpenDialogOptions{Title: "Choose wallpaper folder"})
	}
	var cancel context.CancelFunc
	var closeQuitEndpoint func()
	ready := make(chan struct{})
	return wails.Run(&options.App{
		Title: "Panora", Width: 960, Height: 760, MinWidth: 480, MinHeight: 570,
		BackgroundColour: &options.RGBA{R: 248, G: 247, B: 242, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		SingleInstanceLock: &options.SingleInstanceLock{UniqueId: windowIdentity(socket, runtime.GOOS == "linux" && manager != nil), OnSecondInstanceLaunch: func(instance options.SecondInstanceData) {
			select {
			case <-ready:
				if client.ctx.Err() == nil {
					wailsruntime.WindowUnminimise(client.ctx)
					wailsruntime.WindowShow(client.ctx)
					if slices.Contains(instance.Args, "--about") || slices.Contains(instance.Args, "-about") {
						client.aboutRequested.Store(true)
						notifyAboutLaunch(client.ctx)
					} else if manager != nil {
						go client.ServiceCommand("start")
					}
				}
			case <-time.After(5 * time.Second):
			}
		}},
		OnStartup: func(ctx context.Context) {
			client.ctx, cancel = context.WithCancel(ctx)
			closeQuitEndpoint = serveDesktopQuit(client.ctx, socket, manager != nil, func() { wailsruntime.Quit(client.ctx) })
			close(ready)
			if manager != nil && !about {
				go client.ServiceCommand("start")
			}
			installNativeWindowReopen()
		},
		OnDomReady: installNativeSettingsMenu,
		OnShutdown: func(context.Context) {
			if closeQuitEndpoint != nil {
				closeQuitEndpoint()
			}
			if cancel != nil {
				cancel()
			}
		},
		Bind: []interface{}{client},
	})
}
