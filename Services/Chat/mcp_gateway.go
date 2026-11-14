package Chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/anyscapelabs/argus/Storage"
)

type ResolvedMCPTarget struct {
	ConnectionID string
	ServerID     string
	ToolName     string
	AccessLevel  string
}

// ResolveAgentMCPTool maps a projected tool name ("gmail__send_email", or a
// labelled account prefix when a server has several accounts) back to the
// connection the calling agent is actually bound to, and checks the call
// against that binding's access tier. The tier check runs here, at the
// gateway, so a read-only agent never reaches a mutating call at all.
func ResolveAgentMCPTool(db *sql.DB, agentID, projectedName string) (*ResolvedMCPTarget, error) {
	parts := strings.SplitN(projectedName, "__", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid MCP tool name %q", projectedName)
	}
	prefix, tool := parts[0], parts[1]

	conns, err := Storage.ListAgentConnectionsForAgent(db, agentID)
	if err != nil {
		return nil, err
	}

	for _, c := range conns {
		if c.ServerID == prefix || slugPrefix(c.ServerID, c.Label) == prefix {
			if err := checkAccessTier(c.AccessLevel, tool); err != nil {
				return nil, err
			}
			return &ResolvedMCPTarget{
				ConnectionID: c.ConnectionID,
				ServerID:     c.ServerID,
				ToolName:     tool,
				AccessLevel:  c.AccessLevel,
			}, nil
		}
	}
	return nil, fmt.Errorf("agent is not connected to integration %q", prefix)
}

// checkAccessTier encodes the tier rules. Tier words in tool names are enough
// of a signal for a first gate: anything that smells like a send or a delete
// is beyond draft, and anything that mutates at all is beyond read. The
// errors carry a structured hint so the model can course-correct on the next
// round instead of retrying a call that will always be refused.
func checkAccessTier(tier, tool string) error {
	t := strings.ToLower(tool)
	isSend := strings.Contains(t, "send") || strings.Contains(t, "delete")
	isWrite := isSend || strings.Contains(t, "create") || strings.Contains(t, "update") || strings.Contains(t, "draft")

	if tier == "read" && isWrite {
		payload, _ := json.Marshal(map[string]any{
			"code":      "ACCESS_TIER_READ_ONLY",
			"retryable": false,
			"reason":    fmt.Sprintf("Tool %s requires draft or full access; this agent has read-only access.", tool),
			"next_step": "Report the findings instead of mutating external state.",
		})
		return fmt.Errorf("%s", string(payload))
	}
	if tier == "draft" && isSend {
		payload, _ := json.Marshal(map[string]any{
			"code":      "APPROVAL_REQUIRED_FOR_SEND",
			"retryable": false,
			"reason":    fmt.Sprintf("Direct send via %s is restricted under draft-tier access.", tool),
			"next_step": "Create a draft instead and note the pending item in your report.",
		})
		return fmt.Errorf("%s", string(payload))
	}
	return nil
}

// slugPrefix builds a disambiguated tool prefix for a second account on one
// server: "gmail" with label "Support Inbox" becomes "gmail_supportinbox".
// Single-account servers keep the bare server id, so the common case keeps
// the short, familiar names.
func slugPrefix(serverID, label string) string {
	s := strings.ToLower(strings.TrimSpace(label))
	if s == "" || s == "default" {
		return serverID
	}
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return serverID
	}
	return serverID + "_" + b.String()
}

// ExecuteReadOnlyBatch runs concurrency-safe read tools in parallel while
// preserving the callers' order: result i always belongs to task i, so tool
// results can be paired back to tool calls by index.
func ExecuteReadOnlyBatch(ctx context.Context, tasks []func(context.Context) (string, error)) ([]string, []error) {
	out := make([]string, len(tasks))
	errs := make([]error, len(tasks))
	var wg sync.WaitGroup
	for i, fn := range tasks {
		wg.Add(1)
		go func(idx int, run func(context.Context) (string, error)) {
			defer wg.Done()
			out[idx], errs[idx] = run(ctx)
		}(i, fn)
	}
	wg.Wait()
	return out, errs
}
