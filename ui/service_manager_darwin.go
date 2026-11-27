//go:build darwin

package main

import (
	"os"
	"path/filepath"

	"github.com/vadim82/panora/internal/appservice"
)

func managedService(socket string, explicitSocket bool) (serviceManager, error) {
	if explicitSocket {
		return nil, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	directory := filepath.Dir(executable)
	// Development binaries and explicit socket trials remain connect-only.
	if filepath.Base(directory) != "MacOS" || filepath.Base(filepath.Dir(directory)) != "Contents" || filepath.Ext(filepath.Dir(filepath.Dir(directory))) != ".app" {
		return nil, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	service := appservice.HomebrewLaunchPath(filepath.Join(directory, "panora"))
	manager, err := appservice.New(appservice.Options{Executable: service, Home: home, Socket: socket})
	if err != nil {
		return nil, err
	}
	return manager, nil
}
