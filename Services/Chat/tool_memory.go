package Chat

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Services/Memory"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

// runRememberTool saves one durable fact under a stable key.
func runRememberTool(
	s *Service,
	ctx context.Context,
	runID string,
	agentID string,
	_ string,
	call Protocol.ToolCall,
	result resultFn,
	activity activityFn,
) Protocol.Block {
	var input struct {
		Key      string `json:"key"`
		Value    string `json:"value"`
		Scope    string `json:"scope"`
		Evidence string `json:"evidence"`
	}
	if Shared.IsUnattended(ctx) {
		return result("memory cannot be changed by an unattended or scheduled run", true)
	}
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return result("invalid tool input: could not parse the remember arguments", true)
	}
	source, found, sourceErr := s.Core.DB.GetRequestSource(runID)
	if sourceErr != nil || !found || source.AgentID != agentID || source.Role != "user" || source.Kind != "" || !containsEvidence(source.Text, input.Evidence) {
		return result("memory was not saved: evidence must be exact words from the current human message", true)
	}
	k, kerr := Memory.NormalizeKey(input.Key)
	if kerr != nil {
		return result("invalid memory key: "+kerr.Error(), true)
	}
	activity("tool_started", k)
	confidence := 0.95
	if _, exists, _ := s.Core.DB.GetMemory(agentID, k); exists {
		confidence = 1
	}
	if _, err := Memory.SaveStructured(s.Core.DB, Storage.Memory{
		AgentID: agentID, Key: input.Key, Value: input.Value, Scope: input.Scope,
		Confidence: confidence, SourceType: "verified_user_message",
		SourceMessageID: source.MessageID, SourceRunID: runID,
	}); err != nil {
		activity("tool_finished", k)
		return result("could not save the memory: "+err.Error(), true)
	}
	activity("tool_finished", k)
	return result("Saved under key \""+k+"\". It will appear in your context from now on.", false)
}

// runForgetTool deletes a saved memory by its key.
func runForgetTool(
	s *Service,
	ctx context.Context,
	runID string,
	agentID string,
	_ string,
	call Protocol.ToolCall,
	result resultFn,
	activity activityFn,
) Protocol.Block {
	var input struct {
		Key      string `json:"key"`
		Evidence string `json:"evidence"`
	}
	if Shared.IsUnattended(ctx) {
		return result("memory cannot be changed by an unattended or scheduled run", true)
	}
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return result("invalid tool input: could not parse the forget arguments", true)
	}
	source, foundSource, sourceErr := s.Core.DB.GetRequestSource(runID)
	if sourceErr != nil || !foundSource || source.AgentID != agentID || source.Role != "user" || source.Kind != "" || !containsEvidence(source.Text, input.Evidence) {
		return result("memory was not forgotten: evidence must be exact words from the current human message", true)
	}
	k, err := Memory.NormalizeKey(input.Key)
	if err != nil {
		return result("invalid memory key: "+err.Error(), true)
	}
	activity("tool_started", k)
	found, err := Memory.ForgetStructured(s.Core.DB, agentID, input.Key)
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

func runRecallMemoriesTool(
	s *Service,
	_ context.Context,
	_ string,
	agentID string,
	_ string,
	call Protocol.ToolCall,
	result resultFn,
	activity activityFn,
) Protocol.Block {
	var input struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return result("invalid tool input: could not parse recall_memories arguments", true)
	}
	if input.Limit <= 0 || input.Limit > 12 {
		input.Limit = 8
	}
	activity("tool_started", strings.TrimSpace(input.Query))
	selection, err := Memory.Select(s.Core.DB, agentID, input.Query, 1200, input.Limit, strings.TrimSpace(input.Query) == "")
	activity("tool_finished", strings.TrimSpace(input.Query))
	if err != nil {
		return result("could not recall memories: "+err.Error(), true)
	}
	if selection.Rendered == "" {
		return result("No relevant memories are saved.", false)
	}
	return result(selection.Rendered, false)
}

func containsEvidence(message, evidence string) bool {
	evidence = strings.Join(strings.Fields(evidence), " ")
	if evidence == "" {
		return false
	}
	message = strings.Join(strings.Fields(message), " ")
	return strings.Contains(strings.ToLower(message), strings.ToLower(evidence))
}
