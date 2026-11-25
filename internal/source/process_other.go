//go:build !linux && !darwin

package source

import "os/exec"

func configureProcess(cmd *exec.Cmd) {}
func cleanupProcess(cmd *exec.Cmd)   {}
