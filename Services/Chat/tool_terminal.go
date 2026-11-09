package Chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Services/Permission"
	"github.com/anyscapelabs/argus/Services/Terminal"
)

// runTerminalTool runs one shell command behind the permission card.
func runTerminalTool(
	s *Service,
	ctx context.Context,
	_ string,
	agentID, agentName string,
	call Protocol.ToolCall,
	result resultFn,
	activity activityFn,
) Protocol.Block {
	var input struct {
		Command        string `json:"command"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return result("invalid tool input: could not parse the command arguments", true)
	}
	command := strings.TrimSpace(input.Command)
	if command == "" {
		return result("invalid tool input: command is required", true)
	}

	decision, err := Permission.Request(ctx, s.Permissions, agentID, agentName, Permission.TerminalTool, command)
	if err != nil {
		return result("the permission request was cancelled", true)
	}
	if decision == Permission.Deny {
		return result("The user declined terminal access for this command. Do not retry it; continue without running commands.", true)
	}

	activity("tool_started", command)
	res := Terminal.Run(ctx, command, input.TimeoutSeconds)
	activity("tool_finished", command)
	var b strings.Builder
	b.WriteString(fmt.Sprintf("exit code: %d\n", res.ExitCode))
	if res.DurationMs > 0 {
		b.WriteString(fmt.Sprintf("duration: %d ms\n", res.DurationMs))
	}
	b.WriteString("\n")
	b.WriteString(res.Output)
	text := b.String()
	if res.Err != "" {
		return result("command failed: "+res.Err+"\n"+res.Output, true)
	}
	if res.TimedOut {
		return result(text, true)
	}
	return result(text, false)
}
