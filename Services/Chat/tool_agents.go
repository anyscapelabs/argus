package Chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anyscapelabs/argus/Storage"
	"github.com/google/uuid"
)

// maxDelegationHops bounds how deep a task may travel through teammate hand-
// offs. A chain longer than this almost always means two agents are bouncing
// a task back and forth; at the limit the runner must finish or escalate to
// the user instead of delegating again.
const maxDelegationHops = 4

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
func HandleSaveAgentNote(db *sql.DB, agentID, key, value string) (string, error) {
	k := strings.TrimSpace(key)
	if k == "" {
		return "", fmt.Errorf("key is required")
	}
	if strings.TrimSpace(value) == "" {
		if err := Storage.DeleteAgentMemory(db, agentID, k); err != nil {
			return "", err
		}
		return `{"status":"deleted"}`, nil
	}
	if err := Storage.SetAgentMemory(db, agentID, k, strings.TrimSpace(value)); err != nil {
		return "", err
	}
	return `{"status":"saved"}`, nil
}
