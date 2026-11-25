//go:build !darwin && !linux

package main

func managedService(string, bool) (serviceManager, error) { return nil, nil }
