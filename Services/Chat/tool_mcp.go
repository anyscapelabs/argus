package Chat

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/anyscapelabs/argus/Protocol"
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
			}
		},
		"required": ["server_id"]
	}`),
}

// integrationIDs is the connector list as the catalog knows it today.
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
	def.Description = "Request the user to connect an MCP integration. Use this when the user wants to connect one of: " + list + "."
	def.Schema = json.RawMessage(fmt.Sprintf(`{
		"type": "object",
		"properties": {
			"server_id": {
				"type": "string",
				"description": "One of: %s"
			}
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

// mcpServerPrefix is the server part of a connector tool name, including the
// separator. Chat owns the full spelling and truncates at the provider's
// 64-character limit; the id alone never reaches that, so the replacement
// here is enough for history scanning and the loader to agree on a name.
func mcpServerPrefix(serverID string) string {
	return strings.NewReplacer("-", "_", ".", "_").Replace(serverID) + "__"
}

// mcpLoadToolName is the lazy-loading meta tool. A connector's tool schemas
// are not attached to a request until a turn actually needs them: five
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

// mcpServerToolDefs is a connector's schemas only — what the loader attaches
// when a turn asks for one integration rather than all of them.
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

// mcpServersReferenced scans replayed history for tool_use blocks named after
// a connector and reports which integrations the recent turns actually
// touched. A turn that loaded Gmail and is followed by "also reply to that
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

// connectedServerIDs lists the connectors that have published tools right
// now — the set history scanning may re-attach schemas for.
func (s *Service) connectedServerIDs() []string {
	if s.MCP == nil {
		return nil
	}
	return s.MCP.ListServerIDs()
}

// runMCPLoad answers a load_mcp_tools call. On success it hands back the
// connector's schemas — the agent loop attaches them to the request before
// the next model round, which is what makes those tools callable at all.
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
		return serverID, nil, result(serverID+" is connected but published no tools; there is nothing to load.", false)
	}
	return serverID, nil, result("Not connected: no connection for "+serverID+". "+
		"If the user wants it connected, call connect_mcp with this server_id.", true)
}

// runMCPDynamicTool executes a call whose name belongs to a connector
// (server-prefixed). It reports false when no published tool matches, leaving
// the unknown-tool error to the registry.
func (s *Service) runMCPDynamicTool(call Protocol.ToolCall, result resultFn) (bool, Protocol.Block) {
	if s.MCP == nil {
		return false, Protocol.Block{}
	}
	for _, t := range s.MCP.ListTools() {
		if mcpToolName(t.ServerID, t.Name) == call.Name {
			var args map[string]interface{}
			if strings.TrimSpace(call.Input) != "" {
				_ = json.Unmarshal([]byte(call.Input), &args)
			}
			argsJSON, _ := json.Marshal(args)
			out, err := s.MCP.CallTool(t.ServerID, t.Name, string(argsJSON))
			if err != nil {
				return true, result("integration call failed: "+err.Error(), true)
			}
			if out == "" {
				out = "{}"
			}
			return true, result(out, false)
		}
	}
	return false, Protocol.Block{}
}

// runConnectMCPTool requests a connector connection card from the user.
func runConnectMCPTool(
	s *Service,
	_ context.Context,
	_ string,
	agentID, agentName string,
	call Protocol.ToolCall,
	result resultFn,
	_ activityFn,
) Protocol.Block {
	var input struct {
		ServerID string `json:"server_id"`
	}
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return result("invalid tool input: could not parse the connect_mcp arguments", true)
	}
	switch s.MCPConnect.Register(agentID, agentName, strings.TrimSpace(input.ServerID)) {
	case "shown":
		return result("A connection card is on screen. The user must press Connect and finish sign-in in their browser. Do not say it is connected; stop and wait — the app gives you a new turn when it settles.", false)
	case "pending":
		return result("A connection card is already waiting for the user. Do not request another.", false)
	case "connected":
		return result("That integration is already connected. Use its tools.", false)
	default:
		return result("Unknown integration id. Valid ids: "+strings.Join(s.integrationIDs(), ", ")+".", true)
	}
}
