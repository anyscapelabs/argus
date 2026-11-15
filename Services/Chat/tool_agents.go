package Chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Services/Memory"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
	"github.com/google/uuid"
)

// maxDelegationHops bounds how deep a task may travel through teammate hand-
// offs. A chain longer than this almost always means two agents are bouncing
// a task back and forth; at the limit the runner must finish or escalate to
// the user instead of delegating again.
const maxDelegationHops = 4

var listAgentsToolDef = Protocol.ToolDef{
	Name:        "list_agents",
	Description: "List teammate agents, their @handles, and persisted specialties before delegating work.",
	Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
}

var messageAgentToolDef = Protocol.ToolDef{
	Name:        "message_agent",
	Description: "Delegate a bounded task to a teammate by @handle. The teammate uses only its own accounts and permissions. Set report_back to receive its result.",
	Schema: json.RawMessage(`{
		"type":"object",
		"properties":{
			"to":{"type":"string","description":"Teammate @handle"},
			"task":{"type":"string","description":"Self-contained task brief"},
			"report_back":{"type":"boolean"},
			"urgent":{"type":"boolean"}
		},
		"required":["to","task"]
	}`),
}

var saveAgentNoteToolDef = Protocol.ToolDef{
	Name:        "save_agent_note",
	Description: "Save an agent-only SOP/domain note, or a scratch note shared only within the current delegation thread. Never use this for global user preferences. Empty value deletes it.",
	Schema: json.RawMessage(`{
		"type":"object",
		"properties":{
			"key":{"type":"string"},
			"value":{"type":"string"},
			"scope":{"type":"string","enum":["agent","thread"],"description":"agent is durable for yourself; thread is shared only with this delegation chain"}
		},
		"required":["key","scope"]
	}`),
}

var setMyRoleToolDef = Protocol.ToolDef{
	Name:        "set_my_role",
	Description: "Persist your specialist role after the user explicitly describes your job during onboarding. Reuse it when the user changes that job.",
	Schema: json.RawMessage(`{
		"type":"object",
		"properties":{
			"role":{"type":"string","description":"Short specialty visible to teammates"},
			"instructions":{"type":"string","description":"Optional durable operating instructions for this role"}
		},
		"required":["role"]
	}`),
}

type MessageAgentInput struct {
	To         string `json:"to"`
	Task       string `json:"task"`
	ReportBack bool   `json:"report_back"`
	Urgent     bool   `json:"urgent"`
}

func HandleListAgents(db *sql.DB, callerID string) (string, error) {
	mates, err := Storage.ListAgentTeammates(db, callerID)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(mates)
	return string(raw), err
}

// Dispatch is fire-and-forget: the call returns as soon as the
// message is durable, and the delegator learns the outcome from a report
// message drained at a later round boundary (when report_back was set).
func HandleMessageAgent(
	ctx context.Context,
	db *sql.DB,
	callerID string,
	threadID string,
	currentHops int,
	rawArgs string,
	wakeFn func(agentID string),
) (string, error) {
	if currentHops >= maxDelegationHops {
		return "", fmt.Errorf("delegation hop limit (%d) reached; complete or escalate to user", maxDelegationHops)
	}

	var in MessageAgentInput
	if err := json.Unmarshal([]byte(rawArgs), &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(in.Task) == "" {
		return "", fmt.Errorf("task is required")
	}

	target, err := Storage.GetAgentByHandle(db, in.To)
	if err != nil {
		return "", fmt.Errorf("unknown teammate handle %q", in.To)
	}
	if target.ID == callerID {
		return "", fmt.Errorf("cannot delegate task to self")
	}

	if threadID == "" {
		threadID = "thr_" + uuid.NewString()
	}
	msgID := "amsg_" + uuid.NewString()

	msg := Storage.AgentMessage{
		ID:          msgID,
		ThreadID:    threadID,
		FromAgentID: callerID,
		ToAgentID:   target.ID,
		Kind:        "request",
		Body:        strings.TrimSpace(in.Task),
		Status:      "queued",
		ReportBack:  in.ReportBack,
		Urgent:      in.Urgent,
		Hops:        currentHops + 1,
	}
	if err := Storage.EnqueueAgentMessage(db, msg); err != nil {
		return "", err
	}

	if wakeFn != nil {
		go wakeFn(target.ID)
	}

	res, _ := json.Marshal(map[string]any{
		"status":     "dispatched",
		"thread_id":  threadID,
		"message_id": msgID,
		"to_handle":  target.Handle,
	})
	return string(res), nil
}

// An empty value deletes the key, which is how the tool expresses "forget this
// rule".
func HandleSaveAgentNote(db *sql.DB, agentID, threadID, key, value, scope string) (string, error) {
	k, err := Memory.NormalizeKey(key)
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	switch scope {
	case "thread":
		if strings.TrimSpace(threadID) == "" {
			return "", fmt.Errorf("thread notes require an active delegation thread")
		}
		if value == "" {
			err = Storage.DeleteThreadScratch(db, threadID, k)
		} else {
			err = Storage.UpsertThreadScratch(db, threadID, k, value)
		}
	case "agent", "":
		if value == "" {
			err = Storage.DeleteAgentNote(db, agentID, k)
		} else {
			err = Storage.UpsertAgentNote(db, agentID, k, value)
		}
	default:
		return "", fmt.Errorf("scope must be agent or thread")
	}
	if err != nil {
		return "", err
	}
	if value == "" {
		return `{"status":"deleted"}`, nil
	}
	return `{"status":"saved"}`, nil
}

func HandleSetAgentRole(db *sql.DB, agentID, role, instructions string) (string, error) {
	role = strings.Join(strings.Fields(role), " ")
	if role == "" {
		return "", fmt.Errorf("role is required")
	}
	if len([]rune(role)) > 160 {
		return "", fmt.Errorf("role is limited to 160 characters")
	}
	if len([]rune(instructions)) > 2000 {
		return "", fmt.Errorf("role instructions are limited to 2000 characters")
	}
	if err := Storage.UpdateAgentRole(db, agentID, role, strings.TrimSpace(instructions)); err != nil {
		return "", err
	}
	raw, _ := json.Marshal(map[string]string{"status": "saved", "role": role})
	return string(raw), nil
}

func runTeamTool(
	s *Service,
	ctx context.Context,
	_ string,
	agentID, _ string,
	call Protocol.ToolCall,
	result resultFn,
	_ activityFn,
) Protocol.Block {
	run := Shared.AgentRun(ctx)
	out, err := DispatchBuiltInTeamTool(
		ctx, s.Core.DB.SQL(), agentID, run.ThreadID, run.Hops, call.Name, call.Input, s.WakeAgent,
	)
	if err != nil {
		return result(err.Error(), true)
	}
	return result(out, false)
}
