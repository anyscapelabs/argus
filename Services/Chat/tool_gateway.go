package Chat

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Services/Shared"
)

func gatewayToolDefs() []Protocol.ToolDef {
	return []Protocol.ToolDef{
		conversationReadToolDef,
		webSearchToolDef,
		liveSearchToolsDef,
		liveInvokeToolDef,
	}
}

// runTypedGatewayTool executes one of the two meta-tools for a normal chat run.
// The outer id/name must be preserved so provider tool-call pairing remains
// valid even though the backend executes an inner tool with its real name.
func (s *Service) runTypedGatewayTool(
	ctx context.Context,
	runID, agentID, agentName string,
	discovered map[string]bool,
	outer Protocol.ToolCall,
) (Protocol.Block, string) {
	result := func(text string, isError bool) Protocol.Block {
		return Protocol.Block{
			Type: Protocol.BlockToolResult, ToolRefID: outer.ID,
			ToolName: outer.Name, Text: text, IsError: isError,
		}
	}
	if !outer.Valid {
		return result(invalidCallMessage(outer.Name), true), ""
	}
	if outer.Name == liveSearchToolsDef.Name {
		found := s.searchAvailableTools(discovered, Shared.IsScheduledFire(ctx), !Shared.IsUnattended(ctx), outer.Input)
		if found.Error != nil {
			return result(found.Error.Message, true), ""
		}
		return result(found.Output, found.IsError), ""
	}
	if outer.Name != liveInvokeToolDef.Name {
		return result("unknown gateway tool: "+outer.Name, true), ""
	}
	var input struct {
		Name          string `json:"name"`
		ArgumentsJSON string `json:"arguments_json"`
	}
	if err := json.Unmarshal([]byte(outer.Input), &input); err != nil {
		return result("invalid invoke_tool arguments", true), ""
	}
	input.Name = strings.TrimSpace(input.Name)
	if !discovered[input.Name] {
		return result("tool was not returned by search_tools in this run: "+input.Name, true), input.Name
	}
	arguments := strings.TrimSpace(input.ArgumentsJSON)
	if arguments == "" {
		arguments = "{}"
	}
	inner := s.runTool(ctx, runID, agentID, agentName, Protocol.ToolCall{
		ID: outer.ID, Name: input.Name, Input: arguments, Valid: json.Valid([]byte(arguments)),
	})
	inner.ToolRefID = outer.ID
	inner.ToolName = outer.Name
	return inner, input.Name
}
