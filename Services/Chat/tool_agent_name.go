package Chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

var setAgentNameToolDef = Protocol.ToolDef{
	Name: "set_agent_name",
	Description: "Save the display name the user has chosen for you, or approved for you to use. " +
		"Call it once the user states or confirms a name; it takes effect immediately across the app. " +
		"Do not rename yourself without the user asking or approving, and never during unattended runs.",
	Schema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "The user-chosen display name, 1-40 characters"}
		},
		"required": ["name"]
	}`),
}

func runAgentNameTool(
	s *Service,
	ctx context.Context,
	_ string,
	agentID, _ string,
	call Protocol.ToolCall,
	result resultFn,
	activity activityFn,
) Protocol.Block {
	var input struct {
		Name string `json:"name"`
	}
	if Shared.IsUnattended(ctx) {
		return result("the agent name cannot be changed by an unattended or scheduled run", true)
	}
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return result("invalid tool input: could not parse the set_agent_name arguments", true)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return result("set_agent_name needs a name", true)
	}
	if utf8.RuneCountInString(name) > 40 {
		return result("agent names are limited to 40 characters", true)
	}
	activity("tool_started", name)

	handle, err := saveAgentName(s.Core.DB, agentID, name)
	if err != nil {
		return result(err.Error(), true)
	}
	s.Core.EmitEvent(Shared.EventSlotsChanged, nil)
	raw, _ := json.Marshal(map[string]string{"name": name, "handle": handle})
	return result(string(raw), false)
}

// saveAgentName writes the display name and its @handle. The set_agent_name
// tool and the onboarding card both go through here, so the two can never
// disagree about what a rename touches. The handle rides along so @mentions
// keep matching the new name; it is slugged and de-duplicated by storage.
func saveAgentName(db *Storage.DB, agentID, name string) (string, error) {
	profile, found, err := db.GetAgentProfile(agentID)
	if err != nil {
		return "", fmt.Errorf("could not read the agent profile: %w", err)
	}
	if !found {
		profile = Storage.AgentProfile{ID: agentID, Avatar: Storage.DefaultAgentAvatar}
	}
	profile.AgentName = name
	if err := db.SaveAgentProfile(profile); err != nil {
		return "", fmt.Errorf("could not save the agent name: %w", err)
	}
	handle, herr := Storage.SetAgentHandle(db.SQL(), agentID, name)
	if herr != nil {
		handle = ""
	}
	return handle, nil
}
