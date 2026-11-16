package Chat

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"

	"github.com/anyscapelabs/argus/Services/MCP"
	"github.com/anyscapelabs/argus/Storage"
)

// An empty URL means the MCP server manages its own OAuth browser flow at startup.
//
// Populate this only when the integration requires the user to visit a
// fully-formed URL (with client_id, scope, redirect_uri, etc.) before the MCP
// server can be started. Raw OAuth endpoint base URLs without those parameters
// will be rejected by the provider (400 invalid_request).
func mcpAuthURL(serverID string) string {
	// All currently supported integrations (Gmail, Google Calendar, Todoist)
	// use MCP servers that handle their own OAuth browser pop-up at startup.
	// No separate URL is needed here.
	_ = serverID
	return ""
}

// Only the agent's bound MCP connections may be projected, so one agent's tool
// list never exposes another agent's account. When a
// server has a single connection the tools keep the bare server prefix; with
// several accounts, each connection's tools are labelled with the account so
// the model can tell two inboxes apart. Bound-but-unreachable connections are
// skipped rather than failing the whole projection.
func BuildAgentMCPTools(ctx context.Context, db *sql.DB, mgr *MCP.Manager, agentID string) ([]ToolSpec, error) {
	conns, err := Storage.ListAgentConnectionsForAgent(db, agentID)
	if err != nil {
		return nil, err
	}
	if len(conns) == 0 {
		return nil, nil
	}

	prefixes := connectionPrefixes(conns)

	var specs []ToolSpec
	for _, c := range conns {
		if c.Status != "connected" {
			continue
		}
		client, err := mgr.GetConnectionClient(ctx, c.ServerID, c.ConnectionID)
		if err != nil {
			continue
		}

		for _, t := range client.Tools() {
			desc := t.Description
			if c.AccountEmail != "" {
				desc = fmt.Sprintf("[%s | access:%s] %s", c.AccountEmail, c.AccessLevel, desc)
			}
			specs = append(specs, ToolSpec{
				Name:        projectedMCPToolName(prefixes[c.ConnectionID], t.Name),
				Description: desc,
				InputSchema: t.InputSchema,
			})
		}
	}
	return specs, nil
}

func connectionPrefixes(conns []Storage.AgentConnection) map[string]string {
	serverCount := map[string]int{}
	for _, conn := range conns {
		serverCount[conn.ServerID]++
	}
	out := make(map[string]string, len(conns))
	used := map[string]bool{}
	for _, conn := range conns {
		prefix := normalizeMCPPrefix(conn.ServerID)
		if serverCount[conn.ServerID] > 1 {
			prefix = slugPrefix(conn.ServerID, conn.Label)
		}
		if used[prefix] {
			suffix := strings.TrimPrefix(conn.ConnectionID, "conn_")
			if len(suffix) > 8 {
				suffix = suffix[len(suffix)-8:]
			}
			prefix += "_" + normalizeMCPPrefix(suffix)
		}
		used[prefix] = true
		out[conn.ConnectionID] = prefix
	}
	return out
}

func projectedMCPToolName(prefix, tool string) string {
	name := prefix + "__" + tool
	if len(name) > 64 {
		sum := sha256.Sum256([]byte(name))
		suffix := fmt.Sprintf("_%x", sum[:4])
		name = name[:64-len(suffix)] + suffix
	}
	return name
}

func normalizeMCPPrefix(value string) string {
	return strings.NewReplacer("-", "_", ".", "_").Replace(strings.ToLower(strings.TrimSpace(value)))
}
