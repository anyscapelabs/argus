//go:build !windows

package Browser

import (
	"os/exec"
	"syscall"
	"time"
)

func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateGroup(i *instance) bool {
	if err := syscall.Kill(-i.pid, syscall.SIGTERM); err != nil {
		return false
	}
	done := make(chan struct{})
	go func() {
		_ = i.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(3 * time.Second):
		_ = syscall.Kill(-i.pid, syscall.SIGKILL)
		_ = i.cmd.Wait()
		return true
	}
}

func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
