package Chat

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"unicode"

	"github.com/anyscapelabs/argus/Services/MCP"
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

	prefixes := connectionPrefixes(conns)
	for _, c := range conns {
		if prefixes[c.ConnectionID] == prefix {
			if c.Status != "connected" {
				return nil, fmt.Errorf("account %s requires reconnection before use", c.Label)
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

// checkAccessTier trusts annotations only to narrow access. Missing hints fail
// closed for read and draft; full access is the explicit escape hatch.
func checkAccessTier(tier string, tool MCP.RemoteTool) error {
	if tier == "full" {
		return nil
	}
	restricted := restrictedDraftAction(tool.Name)
	if tier == "read" && tool.ReadOnly && !restricted {
		return nil
	}
	if tier == "draft" && !restricted && (tool.ReadOnly || (tool.Destructive != nil && !*tool.Destructive)) {
		return nil
	}
	code := "ACCESS_TIER_READ_ONLY"
	next := "Use a read-only tool or report what access is required."
	if tier == "draft" {
		code = "ACCESS_TIER_DRAFT_ONLY"
		next = "Create a non-destructive draft when the server explicitly marks that operation safe, or ask for full access."
	}
	return fmt.Errorf(`{"code":%q,"retryable":false,"reason":%q,"next_step":%q}`,
		code, fmt.Sprintf("Tool %s is not allowed with %s access.", tool.Name, tier), next)
}

func restrictedDraftAction(name string) bool {
	var words strings.Builder
	var previousLower bool
	for _, r := range name {
		if unicode.IsUpper(r) && previousLower {
			words.WriteByte(' ')
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			words.WriteRune(unicode.ToLower(r))
			previousLower = unicode.IsLower(r)
		} else {
			words.WriteByte(' ')
			previousLower = false
		}
	}
	blocked := map[string]bool{
		"send": true, "reply": true, "publish": true, "post": true, "submit": true,
		"delete": true, "remove": true, "trash": true, "destroy": true, "archive": true,
		"purge": true, "revoke": true, "cancel": true, "move": true, "spam": true,
	}
	for _, word := range strings.Fields(words.String()) {
		if blocked[word] {
			return true
		}
	}
	return false
}

// slugPrefix builds a disambiguated tool prefix for a second account on one
// server: "gmail" with label "Support Inbox" becomes "gmail_supportinbox".
// Single-account servers keep the bare server id, so the common case keeps
// the short, familiar names.
func slugPrefix(serverID, label string) string {
	s := strings.ToLower(strings.TrimSpace(label))
	if s == "" || s == "default" {
		return normalizeMCPPrefix(serverID)
	}
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return normalizeMCPPrefix(serverID)
	}
	return normalizeMCPPrefix(serverID) + "_" + b.String()
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
