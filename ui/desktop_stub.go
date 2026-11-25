//go:build !wails

package main

import "fmt"

func runDesktop(string, serviceManager, string, bool) error {
	return fmt.Errorf("native client requires a Wails build: see ui/README.md")
}
