package Terminal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const (
	DefaultTimeoutSeconds = 60
	MaxTimeoutSeconds     = 300
	// maxOutput keeps provider context safe; long jobs are truncated in the
	// middle with a marker so both the start and the end survive.
	maxOutput = 32_000
)

// Result is everything the model needs about one command execution.
type Result struct {
	Output     string
	Err        string
	ExitCode   int
	DurationMs int64
	TimedOut   bool
}

// Run executes a shell command and returns combined output. The caller is
// responsible for having obtained permission; the context must come from the
// owning run so stopping a run also kills the command.
func Run(ctx context.Context, command string, timeoutSeconds int) Result {
	if strings.TrimSpace(command) == "" {
		return Result{Err: "empty command"}
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = DefaultTimeoutSeconds
	}
	if timeoutSeconds > MaxTimeoutSeconds {
		timeoutSeconds = MaxTimeoutSeconds
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(runCtx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(runCtx, "sh", "-c", command)
		// Own process group so a timeout kills the whole tree (for example
		// "sleep" and its grandchildren) instead of waiting for them to exit.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error {
			if cmd.Process != nil {
				sigErr := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				if sigErr == syscall.ESRCH {
					return os.ErrProcessDone
				}
				return sigErr
			}
			return os.ErrProcessDone
		}
	}

	start := time.Now()
	out, err := cmd.CombinedOutput()
	res := Result{DurationMs: time.Since(start).Milliseconds()}

	if runCtx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
	}
	if codeErr, ok := err.(*exec.ExitError); ok {
		res.ExitCode = codeErr.ExitCode()
	} else if err != nil && !res.TimedOut && ctx.Err() == nil {
		res.Err = err.Error()
		res.ExitCode = -1
	}
	res.Output = truncate(string(out))
	if res.TimedOut {
		res.Output = appendNotice(res.Output, fmt.Sprintf("timed out after %d seconds", timeoutSeconds))
	}
	return res
}

func truncate(s string) string {
	if len(s) <= maxOutput {
		return s
	}
	head := maxOutput * 2 / 3
	tail := maxOutput - head
	return s[:head] + "\n... [output truncated, " + fmt.Sprint(len(s)) + " bytes total] ...\n" + s[len(s)-tail:]
}

func appendNotice(s, notice string) string {
	notice = "\n[command " + notice + "]\n"
	if len(s) == 0 {
		return strings.TrimPrefix(notice, "\n")
	}
	return s + notice
}
