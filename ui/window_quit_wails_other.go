//go:build wails && !linux

package main

import "context"

func serveDesktopQuit(context.Context, string, bool, func()) func() { return nil }
