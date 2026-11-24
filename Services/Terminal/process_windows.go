//go:build windows

package Terminal

import "os/exec"

func configureProcessGroup(cmd *exec.Cmd) {}
