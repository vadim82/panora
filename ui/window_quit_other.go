//go:build !linux

package main

import "fmt"

func quitDesktop(string, string) error { return fmt.Errorf("--quit is only supported on Linux") }
