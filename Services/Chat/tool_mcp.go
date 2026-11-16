package Chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Services/MCP"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
	"strings"
)

var mcpConnectToolDef = Protocol.ToolDef{
	Name:        "connect_mcp",
	Description: "Request the user to connect an MCP integration (e.g., Gmail). Use this when the user wants to connect an integration.",
	Schema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"server_id": {
				"type": "string",
				"description": "Integration id, for example gmail"
			},
			"label": {"type":"string","description":"Account label, required when adding another account, e.g. Support or Legal"},
			"expected_email": {"type":"string","description":"Optional email to pin and verify during sign-in"},
			"connection_id": {"type":"string","description":"Existing machine account to request access to; omit for a new sign-in"},
			"access_level": {"type":"string","enum":["read","draft","full"],"description":"Requested access, defaults to draft"}
		},
		"required": ["server_id"]
	}`),
}

func (s *Service) integrationIDs() []string {
	if s.MCPConnect == nil {
		return nil
	}
	return s.MCPConnect.ServerIDs()
}

// connectToolDef names the available integrations inside the tool itself, so
// adding a connector to the catalog is enough — the model is never told about
// an integration the app cannot connect, and never left guessing about one it
// can.
func (s *Service) connectToolDef() Protocol.ToolDef {
	def := mcpConnectToolDef
	ids := s.integrationIDs()
	if len(ids) == 0 {
		return def
	}
	list := strings.Join(ids, ", ")
	def.Description = "Request user-approved access to an existing account or connect a new MCP account. " +
		"For another account include a unique label and optional expected_email. Available integrations: " + list + "."
	def.Schema = json.RawMessage(fmt.Sprintf(`{
		"type": "object",
		"properties": {
			"server_id": {
				"type": "string",
				"description": "One of: %s"
			},
			"label": {"type":"string","description":"Unique account label, e.g. Support or Legal"},
			"expected_email": {"type":"string","description":"Optional email to pin and verify during sign-in"},
			"connection_id": {"type":"string","description":"Existing account id from Your connected accounts; omit for new OAuth"},
			"access_level": {"type":"string","enum":["read","draft","full"],"description":"Requested access, defaults to draft"}
		},
		"required": ["server_id"]
	}`, list))
	return def
}

func mcpToolName(serverID, tool string) string {
	n := mcpServerPrefix(serverID) + tool
	if len(n) > 64 {
		n = n[:64]
	}
	return n
}

// Chat owns the full tool spelling and truncates at the provider's
// 64-character limit; the id alone never reaches that, so the replacement
// here is enough for history scanning and the loader to agree on a name.
func mcpServerPrefix(serverID string) string {
	return strings.NewReplacer("-", "_", ".", "_").Replace(serverID) + "__"
}

// Connector schemas are not attached until a turn needs them: five
// integrations publish hundreds of schemas at dozens of tokens each, and
// shipping all of them on every request was the dominant line on the input
// bill. When you need a set, call this once with the server_id; the next
// model round carries those schemas and the calls become possible.
const mcpLoadToolName = "load_mcp_tools"

var mcpLoadToolDef = Protocol.ToolDef{
	Name: mcpLoadToolName,
	Description: "Load one connected integration's tools so they can be called. " +
		"Integration tool schemas are NOT attached to this conversation by default. " +
		"Call this as soon as a request needs one, then use the tools. " +
		"Loading is free; the tool names come back immediately; no approval needed.",
	Schema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"server_id": {
				"type": "string",
				"description": "Integration id from the Integrations block, e.g. gmail"
			}
		},
		"required": ["server_id"]
	}`),
}

func (s *Service) mcpServerToolDefs(serverID string) []Protocol.ToolDef {
	var defs []Protocol.ToolDef
	if s.MCP == nil {
		return defs
	}
	for _, t := range s.MCP.ListTools() {
		if t.ServerID != serverID {
			continue
		}
		schema, _ := json.Marshal(t.InputSchema)
		defs = append(defs, Protocol.ToolDef{
			Name:        mcpToolName(t.ServerID, t.Name),
			Description: "[" + t.ServerName + "] " + t.Description,
			Schema:      schema,
		})
	}
	return defs
}

// A turn that loaded Gmail and is followed by "also reply to that
// email" must not find its tools gone: the servers already in the replayed
// transcript are re-attached, and only those.
func mcpServersReferenced(history []Protocol.Message, serverIDs []string) []string {
	if len(serverIDs) == 0 || len(history) == 0 {
		return nil
	}
	prefixes := map[string]string{}
	for _, id := range serverIDs {
		prefixes[mcpServerPrefix(id)] = id
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range history {
		for _, b := range m.Blocks {
			if b.Type != Protocol.BlockToolUse || b.ToolCall == nil {
				continue
			}
			name := b.ToolCall.Name
			for prefix, id := range prefixes {
				if strings.HasPrefix(name, prefix) && !seen[id] {
					seen[id] = true
					out = append(out, id)
				}
			}
		}
	}
	return out
}

func (s *Service) connectedServerIDs() []string {
	if s.MCP == nil {
		return nil
	}
	return s.MCP.ListServerIDs()
}

// The agent loop must attach returned schemas before the next model round;
// otherwise those tools are not callable.
// An already-loaded id is answered with the tool names again but no second
// copy of the schemas.
func (s *Service) runMCPLoad(call Protocol.ToolCall, loaded map[string]bool) (string, []Protocol.ToolDef, Protocol.Block) {
	result := func(text string, isError bool) Protocol.Block {
		return Protocol.Block{
			Type:      Protocol.BlockToolResult,
			ToolRefID: call.ID,
			ToolName:  call.Name,
			Text:      text,
			IsError:   isError,
		}
	}
	if !call.Valid {
		return "", nil, result("invalid tool call: the arguments were not valid JSON", true)
	}
	var input struct {
		ServerID string `json:"server_id"`
	}
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return "", nil, result("invalid tool input: could not parse the load_mcp_tools arguments", true)
	}
	serverID := strings.TrimSpace(input.ServerID)
	if serverID == "" {
		return "", nil, result("invalid tool input: server_id is required", true)
	}
	names := func(defs []Protocol.ToolDef) []string {
		out := make([]string, len(defs))
		for i, d := range defs {
			out[i] = d.Name
		}
		return out
	}
	if loaded[serverID] {
		return serverID, nil, result(fmt.Sprintf(
			"%s's tools are already attached to this conversation: %s",
			serverID, strings.Join(names(s.mcpServerToolDefs(serverID)), ", ")), false)
	}
	defs := s.mcpServerToolDefs(serverID)
	if len(defs) > 0 {
		return serverID, defs, result(fmt.Sprintf(
			"Loaded %d tools for %s. Their schemas are attached now — call them by name:\n- %s",
			len(defs), serverID, strings.Join(names(defs), "\n- ")), false)
	}
	if s.MCP != nil && s.MCP.Manager() != nil && s.MCP.Manager().IsConnected(serverID) {
		// The session is live but the tool cache may be stale or empty (e.g. the
		// server's tools/list failed on first refresh). Refresh once before
		// concluding there is nothing to load.
		if m := s.MCP.Manager(); m != nil {
			m.RefreshTools()
		}
		defs = s.mcpServerToolDefs(serverID)
		if len(defs) > 0 {
			return serverID, defs, result(fmt.Sprintf(
				"Loaded %d tools for %s. Their schemas are attached now — call them by name:\n- %s",
				len(defs), serverID, strings.Join(names(defs), "\n- ")), false)
		}
		return serverID, nil, result(serverID+" is connected but published no tools; there is nothing to load. (The server's tools/list returned no tools or failed; check MCP logs.)", false)
	}
	if s.MCP != nil && s.MCP.HasStoredSession(serverID) {
		return serverID, nil, result(serverID+" is signed in but not connected in this session yet (session restore may still be in progress, or it needs to be reconnected). Ask the user to reconnect it from Integrations, or try again shortly.", true)
	}
	return serverID, nil, result("Not connected: no connection for "+serverID+". "+
		"If the user wants it connected, call connect_mcp with this server_id.", true)
}

func (s *Service) runMCPDynamicTool(ctx context.Context, agentID string, call Protocol.ToolCall, result resultFn) (bool, Protocol.Block) {
	if s.MCP == nil || !strings.Contains(call.Name, "__") {
		return false, Protocol.Block{}
	}
	out, err := ExecuteAgentMCPTool(ctx, s.Core.DB.SQL(), s.MCP.Manager(), agentID, call.Name, call.Input)
	if err != nil {
		if isMCPAuthError(err) {
			s.Core.EmitEvent(Shared.EventIntegrationsChanged, nil)
		}
		return true, result("integration call failed: "+err.Error(), true)
	}
	if out == "" {
		out = "{}"
	}
	return true, result(out, false)
}

func runConnectMCPTool(
	s *Service,
	ctx context.Context,
	_ string,
	agentID, agentName string,
	call Protocol.ToolCall,
	result resultFn,
	_ activityFn,
) Protocol.Block {
	var input struct {
		ServerID      string `json:"server_id"`
		Label         string `json:"label"`
		ExpectedEmail string `json:"expected_email"`
		ConnectionID  string `json:"connection_id"`
		AccessLevel   string `json:"access_level"`
	}
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return result("invalid tool input: could not parse the connect_mcp arguments", true)
	}
	if s.MCPConnect == nil {
		return result("integration connection service is unavailable", true)
	}
	if Shared.IsUnattended(ctx) {
		return result("This run cannot open an account-approval card because no user is present. Report that access is needed, including the integration, account label or email, and requested tier.", true)
	}
	switch s.MCPConnect.Register(
		agentID, agentName, strings.TrimSpace(input.ServerID), strings.TrimSpace(input.Label),
		strings.TrimSpace(input.ExpectedEmail), strings.TrimSpace(input.ConnectionID), strings.TrimSpace(input.AccessLevel),
	) {
	case "shown":
		return result("A connection card is on screen. The user must press Connect and finish sign-in in their browser. Do not say it is connected; stop and wait — the app gives you a new turn when it settles.", false)
	case "pending":
		return result("A connection card is already waiting for the user. Do not request another.", false)
	case "connected":
		return result("That account is already bound to this agent. Use its tools.", false)
	case "duplicate":
		return result("That integration already has an account with this label. Choose the existing connection_id or use a different label.", true)
	default:
		return result("Unknown integration id. Valid ids: "+strings.Join(s.integrationIDs(), ", ")+".", true)
	}
}

// Projected names resolve only through the agent's own bindings, and access is
// checked at the gateway before a call reaches the connection's session.
// Tier refusals surface as structured hints the model can act on.
func ExecuteAgentMCPTool(ctx context.Context, db *sql.DB, mgr *MCP.Manager, agentID, projectedName, rawArgs string) (string, error) {
	target, err := ResolveAgentMCPTool(db, agentID, projectedName)
	if err != nil {
		return "", err
	}

	client, err := mgr.GetConnectionClient(ctx, target.ServerID, target.ConnectionID)
	if err != nil {
		return "", err
	}

	var remote *MCP.RemoteTool
	for _, tool := range client.Tools() {
		if projectedMCPToolName(connectionPrefixFor(db, agentID, target.ConnectionID), tool.Name) == projectedName {
			copy := tool
			remote = &copy
			target.ToolName = tool.Name
			break
		}
	}
	if remote == nil {
		return "", fmt.Errorf("integration tool %q is no longer published by this account", projectedName)
	}
	if err := checkAccessTier(target.AccessLevel, *remote); err != nil {
		return "", err
	}
	res, err := client.CallTool(ctx, target.ToolName, rawArgs)
	if err != nil {
		if isMCPAuthError(err) {
			_ = Storage.SetMCPConnectionStatus(db, target.ConnectionID, "needs_reauth")
		}
		return "", err
	}
	return res, nil
}

func connectionPrefixFor(db *sql.DB, agentID, connectionID string) string {
	connections, _ := Storage.ListAgentConnectionsForAgent(db, agentID)
	return connectionPrefixes(connections)[connectionID]
}

func isMCPAuthError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "invalid_grant") || strings.Contains(text, "unauthorized") ||
		strings.Contains(text, "401") || strings.Contains(text, "token expired")
}
