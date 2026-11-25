package main

import "github.com/vadim82/panora/internal/appservice"

func managedService(socket string, explicitSocket bool) (serviceManager, error) {
	// A development binary stays connect-only even when the package is installed.
	if explicitSocket || !installedLinuxClient() {
		return nil, nil
	}
	manager, err := appservice.NewSystemd(socket)
	if err != nil {
		return nil, err
	}
	return manager, nil
}
