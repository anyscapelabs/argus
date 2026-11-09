package Chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Services/Location"
	"github.com/anyscapelabs/argus/Services/Permission"
	"github.com/anyscapelabs/argus/Services/Shared"
)

// resultFn renders one tool_result block for the model.
type resultFn = func(text string, isError bool) Protocol.Block

// activityFn emits a live status the chat shows while nothing else streams.
type activityFn = func(kind, command string)

// toolRunner is the uniform executor one tool registers as.
type toolRunner func(
	s *Service,
	ctx context.Context,
	runID, agentID, agentName string,
	call Protocol.ToolCall,
	result resultFn,
	activity activityFn,
) Protocol.Block

// toolRunners is the registry: each static tool owns one executor. The MCP
// dynamic namespace stays outside it — those names arrive at runtime and run
// through runMCPDynamicTool below.
var toolRunners = map[string]toolRunner{
	"get_time_and_location": runTimeLocationTool,
	"remember":              runRememberTool,
	"forget":                runForgetTool,
	"schedule_reminder":     runScheduleReminderTool,
	"create_routine":        runCreateRoutineTool,
	"list_scheduled":        runListScheduledTool,
	"cancel_scheduled":      runCancelScheduledTool,
	"web_search":            runWebSearchTool,
	"connect_mcp":           runConnectMCPTool,
	"conversation_read":     runConversationReadTool,
	"write_artifact":        runWriteArtifactTool,
	"browser":               runBrowserTool,
	Permission.TerminalTool: runTerminalTool,
}

// runTool executes one tool_use block and returns the tool_result block the
// model should see. Permission gating happens inside each runner, before
// anything executes.
func (s *Service) runTool(ctx context.Context, runID, agentID, agentName string, call Protocol.ToolCall) Protocol.Block {
	result := resultFn(func(text string, isError bool) Protocol.Block {
		limit := maxResultChars(call.Name)
		if n := len(text); n > limit {
			head := strings.ToValidUTF8(text[:limit], "")
			text = head + fmt.Sprintf("\n[…tool output truncated: showing %d of %d characters]", len(head), n)
		}
		return Protocol.Block{
			Type:      Protocol.BlockToolResult,
			ToolRefID: call.ID,
			ToolName:  call.Name,
			Text:      text,
			IsError:   isError,
		}
	})
	activity := activityFn(func(kind, command string) {
		s.Core.EmitEvent(Shared.EventActivity, Shared.ActivityView{
			AgentID: agentID,
			RunID:   runID,
			Kind:    kind,
			Tool:    call.Name,
			Command: command,
		})
	})

	if !call.Valid {
		return result(invalidCallMessage(call.Name), true)
	}
	if run, ok := toolRunners[call.Name]; ok {
		return run(s, ctx, runID, agentID, agentName, call, result, activity)
	}
	// Connector tools keep their namespace out of the static registry: a
	// server publishing hundreds of schemas must not be re-registered per
	// session, and a collision with a static name loses to the static tool.
	if found, blk := s.runMCPDynamicTool(call, result); found {
		return blk
	}
	return result("unknown tool: "+call.Name, true)
}

// runConversationReadTool adapts the history tool to the registry signature.
func runConversationReadTool(
	s *Service,
	_ context.Context,
	_ string,
	agentID string,
	_ string,
	call Protocol.ToolCall,
	result resultFn,
	activity activityFn,
) Protocol.Block {
	return s.runConversationRead(agentID, call, result, activity)
}

// runWriteArtifactTool adapts the artifact tool to the registry signature.
func runWriteArtifactTool(
	s *Service,
	_ context.Context,
	_ string,
	agentID string,
	_ string,
	call Protocol.ToolCall,
	result resultFn,
	activity activityFn,
) Protocol.Block {
	return s.runArtifactTool(call, agentID, result, activity)
}

// runBrowserTool adapts the browser tool to the registry signature.
func runBrowserTool(
	s *Service,
	ctx context.Context,
	runID, agentID, agentName string,
	call Protocol.ToolCall,
	result resultFn,
	activity activityFn,
) Protocol.Block {
	return s.runBrowser(ctx, runID, agentID, agentName, call, result, activity)
}

// runTimeLocationTool answers get_time_and_location; it is free and needs no
// approval, so the runner runs before the model ever pays a permission round.
func runTimeLocationTool(
	s *Service,
	ctx context.Context,
	_ string,
	_ string,
	_ string,
	_ Protocol.ToolCall,
	result resultFn,
	activity activityFn,
) Protocol.Block {
	activity("tool_started", "")
	info := Location.Current(ctx, s.Core.DB)
	activity("tool_finished", "")
	return result(info.ToolResult(), false)
}
