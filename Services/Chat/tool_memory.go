package Chat

import (
	"context"
	"encoding/json"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Services/Memory"
)

// runRememberTool saves one durable fact under a stable key.
func runRememberTool(
	s *Service,
	_ context.Context,
	_ string,
	_ string,
	_ string,
	call Protocol.ToolCall,
	result resultFn,
	activity activityFn,
) Protocol.Block {
	var input struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return result("invalid tool input: could not parse the remember arguments", true)
	}
	k, kerr := Memory.NormalizeKey(input.Key)
	if kerr != nil {
		return result("invalid memory key: "+kerr.Error(), true)
	}
	activity("tool_started", k)
	if err := Memory.Save(s.Core.DB, input.Key, input.Value); err != nil {
		activity("tool_finished", k)
		return result("could not save the memory: "+err.Error(), true)
	}
	activity("tool_finished", k)
	return result("Saved under key \""+k+"\". It will appear in your context from now on.", false)
}

// runForgetTool deletes a saved memory by its key.
func runForgetTool(
	s *Service,
	_ context.Context,
	_ string,
	_ string,
	_ string,
	call Protocol.ToolCall,
	result resultFn,
	activity activityFn,
) Protocol.Block {
	var input struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return result("invalid tool input: could not parse the forget arguments", true)
	}
	k, err := Memory.NormalizeKey(input.Key)
	if err != nil {
		return result("invalid memory key: "+err.Error(), true)
	}
	activity("tool_started", k)
	found, err := Memory.Forget(s.Core.DB, input.Key)
	if err != nil {
		activity("tool_finished", k)
		return result("could not delete the memory: "+err.Error(), true)
	}
	if !found {
		activity("tool_finished", k)
		return result("No memory is stored under \""+k+"\". Nothing was deleted.", true)
	}
	activity("tool_finished", k)
	return result("Forgot \""+k+"\". It is gone from future context.", false)
}
