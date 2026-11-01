package Chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Router"
	"github.com/anyscapelabs/argus/Services/Permission"
	"github.com/anyscapelabs/argus/Services/Terminal"
)

var terminalToolDef = Protocol.ToolDef{
	Name: Permission.TerminalTool,
	Description: "Run one shell command on the user's machine using /bin/sh. " +
		"Returns combined stdout/stderr and the exit code. " +
		"The user must approve terminal access through the app before the first command runs; " +
		"if they decline you receive an error result and should continue without executing commands.",
	Schema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {
				"type": "string",
				"description": "The shell command to execute"
			},
			"timeout_seconds": {
				"type": "integer",
				"description": "Optional timeout in seconds, between 1 and 300. Default 60."
			}
		},
		"required": ["command"]
	}`),
}

// attachTools turns on tool calling when the model supports it. Returns the
// system prompt suffix describing the tools (empty when tools are off).
func (s *Service) attachTools(req *Protocol.Request, target *Protocol.Target) bool {
	tools := []Protocol.ToolDef{terminalToolDef}
	if !s.modelSupportsTools(target) {
		return false
	}
	req.Tools = tools
	return true
}

func (s *Service) modelSupportsTools(target *Protocol.Target) bool {
	ad, err := Router.AdapterFor(target)
	if err != nil {
		return false
	}
	scratch := &Protocol.Request{Tools: []Protocol.ToolDef{terminalToolDef}}
	Protocol.ClampRequest(scratch, ad.Capabilities(Protocol.ModelInfo{ID: target.ModelID}))
	return len(scratch.Tools) > 0
}

// runTool executes one tool_use block and returns the tool_result block the
// model should see. Permission gating happens before anything executes.
func (s *Service) runTool(ctx context.Context, agentID, agentName string, call Protocol.ToolCall) Protocol.Block {
	result := func(text string, isError bool) Protocol.Block {
		return Protocol.Block{
			Type:      Protocol.BlockToolResult,
			ToolRefID: call.ID,
			ToolName:  call.Name,
			Text:      text,
			IsError:   isError,
		}
	}

	if !call.Valid {
		return result("invalid tool call: the arguments were not valid JSON", true)
	}
	if call.Name != Permission.TerminalTool {
		return result("unknown tool: "+call.Name, true)
	}

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

	res := Terminal.Run(ctx, command, input.TimeoutSeconds)
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
