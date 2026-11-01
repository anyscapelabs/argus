package Chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Router"
	"github.com/anyscapelabs/argus/Services/Permission"
	"github.com/anyscapelabs/argus/Services/Shared"
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

var mcpConnectToolDef = Protocol.ToolDef{
	Name: "connect_mcp",
	Description: "Request the user to connect an MCP integration (e.g., Gmail). Use this when the user wants to connect an integration.",
	Schema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"server_id": {
				"type": "string",
				"description": "Integration id like gmail, google-calendar, todoist"
			}
		},
		"required": ["server_id"]
	}`),
}

func mcpToolName(serverID, tool string) string {
	n := strings.NewReplacer("-", "_", ".", "_").Replace(serverID) + "__" + tool
	if len(n) > 64 {
		n = n[:64]
	}
	return n
}

func (s *Service) mcpToolDefs() []Protocol.ToolDef {
	var defs []Protocol.ToolDef
	if s.MCP == nil {
		return defs
	}
	for _, t := range s.MCP.ListTools() {
		schema, _ := json.Marshal(t.InputSchema)
		defs = append(defs, Protocol.ToolDef{
			Name:        mcpToolName(t.ServerID, t.Name),
			Description: "[" + t.ServerName + "] " + t.Description,
			Schema:      schema,
		})
	}
	return defs
}

// attachTools turns on tool calling when the model supports it. Returns the
// system prompt suffix describing the tools (empty when tools are off).
func (s *Service) attachTools(req *Protocol.Request, target *Protocol.Target) bool {
	tools := append([]Protocol.ToolDef{terminalToolDef, mcpConnectToolDef}, s.mcpToolDefs()...)
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
	scratch := &Protocol.Request{Tools: append([]Protocol.ToolDef{terminalToolDef, mcpConnectToolDef}, s.mcpToolDefs()...)}
	Protocol.ClampRequest(scratch, ad.Capabilities(Protocol.ModelInfo{ID: target.ModelID}))
	return len(scratch.Tools) > 0
}

// runTool executes one tool_use block and returns the tool_result block the
// model should see. Permission gating happens before anything executes.
func (s *Service) runTool(ctx context.Context, runID, agentID, agentName string, call Protocol.ToolCall) Protocol.Block {
	result := func(text string, isError bool) Protocol.Block {
		return Protocol.Block{
			Type:      Protocol.BlockToolResult,
			ToolRefID: call.ID,
			ToolName:  call.Name,
			Text:      text,
			IsError:   isError,
		}
	}
	// activity emits a live status the chat shows while nothing else streams.
	activity := func(kind, command string) {
		s.Core.EmitEvent(Shared.EventActivity, Shared.ActivityView{
			AgentID: agentID,
			RunID:   runID,
			Kind:    kind,
			Tool:    call.Name,
			Command: command,
		})
	}

	if !call.Valid {
		return result("invalid tool call: the arguments were not valid JSON", true)
	}

	if call.Name == "connect_mcp" {
		var input struct {
			ServerID string `json:"server_id"`
		}
		if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
			return result("invalid tool input: could not parse the connect_mcp arguments", true)
		}
		switch s.MCPConnect.Register(agentID, agentName, strings.TrimSpace(input.ServerID)) {
		case "shown":
			return result("A connection card is on screen. The user must press Connect and finish sign-in in their browser. Do not say it is connected; stop and wait.", false)
		case "pending":
			return result("A connection card is already waiting for the user. Do not request another.", false)
		case "connected":
			return result("That integration is already connected. Use its tools.", false)
		default:
			return result("Unknown integration id. Valid ids: gmail, google-calendar, todoist.", true)
		}
	}

	if s.MCP != nil {
		for _, t := range s.MCP.ListTools() {
			if mcpToolName(t.ServerID, t.Name) == call.Name {
				var args map[string]interface{}
				if strings.TrimSpace(call.Input) != "" {
					_ = json.Unmarshal([]byte(call.Input), &args)
				}
				argsJSON, _ := json.Marshal(args)
				out, err := s.MCP.CallTool(t.ServerID, t.Name, string(argsJSON))
				if err != nil {
					return result("integration call failed: "+err.Error(), true)
				}
				if out == "" {
					out = "{}"
				}
				return result(out, false)
			}
		}
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
