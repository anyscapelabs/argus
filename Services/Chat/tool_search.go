package Chat

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Services/Search"
	"github.com/anyscapelabs/argus/Services/Shared"
)

// runWebSearchTool searches the public web and reports the found sites as a
// live activity source while the answer streams into the transcript.
func runWebSearchTool(
	s *Service,
	ctx context.Context,
	runID, agentID string,
	_ string,
	call Protocol.ToolCall,
	result resultFn,
	activity activityFn,
) Protocol.Block {
	var input struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
	}
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return result("invalid tool input: could not parse the web_search arguments", true)
	}
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return result("invalid tool input: query is required", true)
	}

	activity("tool_started", query)
	answer := Search.Search(ctx, query, input.MaxResults)
	// The indicator consumes the found sites right away (favicon plus
	// result titles) instead of echoing the query back to the user.
	s.Core.EmitEvent(Shared.EventActivity, Shared.ActivityView{
		AgentID: agentID,
		RunID:   runID,
		Kind:    "tool_results",
		Tool:    call.Name,
		Sources: activitySources(answer.Results),
	})
	activity("tool_finished", query)
	return result(answer.Render(), answer.Err != "")
}
