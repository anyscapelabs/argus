//go:build windows

package MCP

import (
	"os"
	"os/exec"
)

func execCmd(command string, args []string, env []string) *exec.Cmd {
	cmd := exec.Command(command, args...)
	cmd.Env = append(os.Environ(), env...)
	return cmd
}
