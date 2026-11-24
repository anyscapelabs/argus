//go:build windows

package Browser

import "os/exec"

func configureProcessGroup(cmd *exec.Cmd) {}

func terminateGroup(i *instance) bool {
	return false
}

func processAlive(pid int) bool {
	return true
}
