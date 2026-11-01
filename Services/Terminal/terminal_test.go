package Terminal

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunEcho(t *testing.T) {
	res := Run(context.Background(), "echo argus-terminal-test", 10)
	if res.Err != "" {
		t.Fatalf("unexpected exec error: %s", res.Err)
	}
	if res.TimedOut {
		t.Fatal("unexpected timeout")
	}
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d", res.ExitCode)
	}
	if !strings.Contains(res.Output, "argus-terminal-test") {
		t.Fatalf("output missing marker, got: %q", res.Output)
	}
}

func TestRunFailingCommandReportsExitCode(t *testing.T) {
	res := Run(context.Background(), "sh -c 'exit 3'", 10)
	if res.ExitCode != 3 {
		t.Fatalf("expected exit code 3, got %d", res.ExitCode)
	}
	if res.Err != "" {
		t.Fatalf("expected no exec error for clean exit, got: %s", res.Err)
	}
}

func TestRunTimeout(t *testing.T) {
	start := time.Now()
	res := Run(context.Background(), "sleep 5", 1)
	if !res.TimedOut {
		t.Fatal("expected timeout")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("command should have been killed after ~1s, took %s", elapsed)
	}
	if !strings.Contains(res.Output, "timed out after 1 seconds") {
		t.Fatalf("timeout notice missing, got: %q", res.Output)
	}
}

func TestRunEmptyCommand(t *testing.T) {
	res := Run(context.Background(), "   ", 10)
	if res.Err != "empty command" {
		t.Fatalf("expected empty command error, got: %q", res.Err)
	}
}

func TestRunUnclampsHugeTimeout(t *testing.T) {
	// Just ensure a huge timeout does not panic and the command runs.
	res := Run(context.Background(), "echo ok", 999999)
	if !strings.Contains(res.Output, "ok") {
		t.Fatalf("unexpected output: %q", res.Output)
	}
}
