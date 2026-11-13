package Chat

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/anyscapelabs/argus/Protocol"
	ProtocolShared "github.com/anyscapelabs/argus/Protocol/Shared"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

const liveSessionLifetime = 4 * time.Hour

type liveToolSession struct {
	id         string
	agentID    string
	agentName  string
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	discovered map[string]bool
}

var liveSearchToolsDef = Protocol.ToolDef{
	Name: "search_tools",
	Description: "Find a few relevant Argus tools without loading the full tool catalog. " +
		"Call this when the task needs a capability other than web_search or conversation_read.",
	Schema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "Capability needed, for example save a file, schedule a reminder, browse a site, or send email"},
			"limit": {"type": "integer", "description": "Maximum matching tools to return; default 3, maximum 5"}
		},
		"required": ["query"]
	}`),
}

var liveInvokeToolDef = Protocol.ToolDef{
	Name: "invoke_tool",
	Description: "Execute one exact Argus tool returned by search_tools. " +
		"arguments_json must be a JSON object matching that tool's input_schema.",
	Schema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "Exact tool name returned by search_tools"},
			"arguments_json": {"type": "string", "description": "JSON object encoded as a string and matching the returned input_schema"}
		},
		"required": ["name", "arguments_json"]
	}`),
}

var liveReadSkillDef = Protocol.ToolDef{
	Name: "read_skill",
	Description: "Load the step-by-step workflow, exact tool names, and live schemas for an installed skill. " +
		"Call this BEFORE search_tools or invoke_tool whenever a task matches an installed skill.",
	Schema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "Exact skill id from Installed skills, e.g. gmail, notion, github"}
		},
		"required": ["name"]
	}`),
}

var liveInstallSkillDef = Protocol.ToolDef{
	Name: "install_skill",
	Description: "Enable an available skill for this agent so its workflow instructions can be read. " +
		"Requires user approval via permission card.",
	Schema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "Skill id to install/enable"}
		},
		"required": ["name"]
	}`),
}

func liveFunctionDeclarations() []Shared.LiveFunctionDeclaration {
	defs := []Protocol.ToolDef{
		conversationReadToolDef,
		webSearchToolDef,
		liveSearchToolsDef,
		liveInvokeToolDef,
		liveReadSkillDef,
		liveInstallSkillDef,
	}
	out := make([]Shared.LiveFunctionDeclaration, 0, len(defs))
	for _, def := range defs {
		parameters := map[string]any{}
		_ = json.Unmarshal(ProtocolShared.CleanSchemaForGemini(def.Schema), &parameters)
		uppercaseSchemaTypes(parameters)
		out = append(out, Shared.LiveFunctionDeclaration{
			Name:        def.Name,
			Description: def.Description,
			Behavior:    "NON_BLOCKING",
			Parameters:  parameters,
		})
	}
	return out
}

func uppercaseSchemaTypes(value any) {
	switch node := value.(type) {
	case map[string]any:
		for key, child := range node {
			if key == "type" {
				if text, ok := child.(string); ok {
					node[key] = strings.ToUpper(text)
				}
				continue
			}
			uppercaseSchemaTypes(child)
		}
	case []any:
		for _, child := range node {
			uppercaseSchemaTypes(child)
		}
	}
}

func (s *Service) beginLiveToolSession(agentID, name string) string {
	id := uuid.NewString()
	ctx, cancel := context.WithTimeout(context.Background(), liveSessionLifetime)
	session := &liveToolSession{
		id:         id,
		agentID:    agentID,
		agentName:  name,
		ctx:        ctx,
		cancel:     cancel,
		discovered: map[string]bool{},
	}
	s.liveMu.Lock()
	if s.liveSessions == nil {
		s.liveSessions = map[string]*liveToolSession{}
	}
	s.liveSessions[id] = session
	s.liveMu.Unlock()
	go func() {
		<-ctx.Done()
		s.liveMu.Lock()
		if s.liveSessions[id] == session {
			delete(s.liveSessions, id)
		}
		s.liveMu.Unlock()
	}()
	return id
}

// EndLiveSession cancels any tool or permission wait owned by a finished Live
// call and removes its discovery state.
func (s *Service) EndLiveSession(sessionID string) bool {
	s.liveMu.Lock()
	session := s.liveSessions[strings.TrimSpace(sessionID)]
	if session != nil {
		delete(s.liveSessions, session.id)
	}
	s.liveMu.Unlock()
	if session == nil {
		return false
	}
	session.cancel()
	return true
}

func (s *Service) endAllLiveSessions() {
	s.liveMu.Lock()
	sessions := make([]*liveToolSession, 0, len(s.liveSessions))
	for _, session := range s.liveSessions {
		sessions = append(sessions, session)
	}
	s.liveSessions = nil
	s.liveMu.Unlock()
	for _, session := range sessions {
		session.cancel()
	}
}

func (s *Service) liveToolSession(sessionID string) *liveToolSession {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	return s.liveSessions[strings.TrimSpace(sessionID)]
}

// RunLiveTool is the sole function-call bridge used by the frontend. It keeps
// discovery, allowlisting, permissions, execution and output limits in Go; the
// webview only relays Gemini's call and this result.
func (s *Service) RunLiveTool(sessionID, callID, name, argsJSON string) Shared.LiveToolResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.LiveToolResult{Error: aerr}
	}
	session := s.liveToolSession(sessionID)
	if session == nil {
		return Shared.LiveToolResult{Output: "the live tool session has ended", IsError: true}
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if err := session.ctx.Err(); err != nil {
		return Shared.LiveToolResult{Output: "the live tool session has ended", IsError: true}
	}
	name = strings.TrimSpace(name)
	if callID == "" {
		callID = uuid.NewString()
	}
	switch name {
	case "search_tools":
		return s.searchLiveTools(session, argsJSON)
	case "invoke_tool":
		return s.invokeLiveTool(session, callID, argsJSON)
	case "read_skill":
		return s.readSkillGateway(session.agentID, session.discovered, argsJSON)
	case "install_skill":
		return s.installSkillGateway(session.ctx, session.agentID, session.agentName, argsJSON)
	case "conversation_read", "web_search":
		return s.runLiveCanonicalTool(session, callID, name, argsJSON)
	default:
		return Shared.LiveToolResult{Output: "unknown live gateway function: " + name, IsError: true}
	}
}

func (s *Service) runLiveCanonicalTool(session *liveToolSession, callID, name, argsJSON string) Shared.LiveToolResult {
	argsJSON = strings.TrimSpace(argsJSON)
	if argsJSON == "" {
		argsJSON = "{}"
	}
	block := s.runTool(session.ctx, session.id, session.agentID, session.agentName, Protocol.ToolCall{
		ID: callID, Name: name, Input: argsJSON, Valid: json.Valid([]byte(argsJSON)),
	})
	if !block.IsError && block.FileWrite != nil {
		s.persistLiveFileWrite(session.agentID, block.FileWrite)
	}
	return Shared.LiveToolResult{Output: block.Text, IsError: block.IsError}
}

func (s *Service) persistLiveFileWrite(agentID string, write *Protocol.FileWrite) {
	if write == nil {
		return
	}
	seq, err := s.Core.DB.NextSeq(agentID)
	if err != nil {
		return
	}
	id, _ := uuid.NewV7()
	copy := *write
	_ = s.Core.DB.InsertMessage(Storage.Message{
		ID: id.String(), AgentID: agentID, Seq: seq, Role: "assistant",
		Kind: Storage.MessageKindFileWrite, Status: "complete",
		BlocksJSON: mustMarshal([]Protocol.Block{{Type: Protocol.BlockToolResult, FileWrite: &copy}}),
		CreatedAt:  nowUnixMs(), CompletedAt: nowUnixMs(),
	})
}

func (s *Service) invokeLiveTool(session *liveToolSession, callID, argsJSON string) Shared.LiveToolResult {
	var input struct {
		Name          string `json:"name"`
		ArgumentsJSON string `json:"arguments_json"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return Shared.LiveToolResult{Output: "invalid invoke_tool arguments", IsError: true}
	}
	input.Name = strings.TrimSpace(input.Name)
	if !session.discovered[input.Name] {
		return Shared.LiveToolResult{
			Output:  "tool was not returned by search_tools in this live session: " + input.Name,
			IsError: true,
		}
	}
	return s.runLiveCanonicalTool(session, callID, input.Name, input.ArgumentsJSON)
}

type liveToolMatch struct {
	Name        string          `json:"name"`
	Category    string          `json:"category"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	score       int
}

func (s *Service) searchLiveTools(session *liveToolSession, argsJSON string) Shared.LiveToolResult {
	return s.searchAvailableTools(session.discovered, Shared.IsScheduledFire(session.ctx), false, argsJSON)
}

func (s *Service) searchAvailableTools(discovered map[string]bool, scheduledFire, allowMemoryMutation bool, argsJSON string) Shared.LiveToolResult {
	var input struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return Shared.LiveToolResult{Output: "invalid search_tools arguments", IsError: true}
	}
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return Shared.LiveToolResult{Output: "search_tools requires a capability query", IsError: true}
	}
	if input.Limit <= 0 {
		input.Limit = 3
	}
	if input.Limit > 5 {
		input.Limit = 5
	}

	var matches []liveToolMatch
	for _, def := range s.liveToolCatalog(scheduledFire, allowMemoryMutation) {
		category := liveToolCategory(def.Name)
		score := liveToolScore(query, def, category)
		if score == 0 {
			continue
		}
		matches = append(matches, liveToolMatch{
			Name: def.Name, Category: category, Description: def.Description,
			InputSchema: def.Schema, score: score,
		})
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return matches[i].Name < matches[j].Name
		}
		return matches[i].score > matches[j].score
	})
	if len(matches) > input.Limit {
		matches = matches[:input.Limit]
	}
	for _, match := range matches {
		discovered[match.Name] = true
	}
	payload := struct {
		Tools []liveToolMatch `json:"tools"`
		Note  string          `json:"note"`
	}{
		Tools: matches,
		Note:  "Call invoke_tool with one exact name and an arguments_json object matching input_schema.",
	}
	if len(matches) == 0 {
		payload.Note = "No tool matched this query. Search again with a concrete capability or integration name."
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return Shared.LiveToolResult{Output: "could not encode matching tools", IsError: true}
	}
	return Shared.LiveToolResult{Output: string(raw)}
}

func (s *Service) connectedServerSet() map[string]bool {
	out := map[string]bool{}
	if s.MCP != nil {
		for _, id := range s.MCP.ListServerIDs() {
			out[id] = true
		}
	}
	return out
}

func (s *Service) liveToolCatalog(scheduledFire, allowMemoryMutation bool) []Protocol.ToolDef {
	defs := make([]Protocol.ToolDef, 0, len(s.baseToolDefs()))
	for _, def := range s.baseToolDefs() {
		if def.Name == conversationReadToolDef.Name || def.Name == webSearchToolDef.Name {
			continue
		}
		if scheduledFire && (def.Name == scheduleReminderToolDef.Name || def.Name == createRoutineToolDef.Name) {
			continue
		}
		if (!allowMemoryMutation && (def.Name == rememberToolDef.Name || def.Name == forgetToolDef.Name)) ||
			(scheduledFire && def.Name == askUserToolDef.Name) {
			continue
		}
		defs = append(defs, def)
	}
	if s.MCP == nil {
		return defs
	}
	for _, serverID := range s.MCP.ListServerIDs() {
		defs = append(defs, s.mcpServerToolDefs(serverID)...)
	}
	return defs
}

var toolSynonyms = map[string][]string{
	"email":        {"mail", "message", "inbox", "thread", "draft", "gmail"},
	"mail":         {"email", "message", "inbox", "gmail"},
	"song":         {"track", "music", "playlist", "spotify", "audio"},
	"music":        {"song", "track", "playlist", "spotify"},
	"ticket":       {"issue", "task", "bug", "linear", "jira"},
	"issue":        {"ticket", "bug", "task", "pr", "pull"},
	"bug":          {"error", "exception", "issue", "sentry", "crash"},
	"error":        {"bug", "exception", "sentry", "issue", "log"},
	"doc":          {"document", "page", "note", "notion", "docs"},
	"document":     {"doc", "page", "file", "docs", "notion"},
	"spreadsheet":  {"sheet", "sheets", "table", "cell", "csv"},
	"sheet":        {"spreadsheet", "sheets", "row", "column"},
	"presentation": {"slides", "deck", "slide"},
	"deck":         {"slides", "presentation", "slide"},
	"meeting":      {"calendar", "event", "schedule", "invite"},
	"event":        {"calendar", "meeting", "schedule"},
	"todo":         {"task", "tasks", "todoist", "reminder", "checklist"},
	"task":         {"todo", "tasks", "todoist", "issue"},
	"repo":         {"repository", "github", "branch", "commit", "pr"},
	"pr":           {"pull", "request", "github", "review", "merge"},
	"wiki":         {"confluence", "notion", "page", "knowledge"},
	"worker":       {"cloudflare", "kv", "r2", "d1", "dns", "zone"},
}

func expandQueryVariants(term string) []string {
	seen := map[string]bool{term: true}
	out := []string{term}
	if len(term) > 3 && strings.HasSuffix(term, "s") {
		sing := strings.TrimSuffix(term, "s")
		if !seen[sing] {
			seen[sing] = true
			out = append(out, sing)
		}
	}
	for _, base := range append([]string(nil), out...) {
		for _, syn := range toolSynonyms[base] {
			if !seen[syn] {
				seen[syn] = true
				out = append(out, syn)
			}
		}
	}
	return out
}

func liveToolScore(query string, def Protocol.ToolDef, category string) int {
	name := strings.ToLower(def.Name)
	description := strings.ToLower(def.Description)
	category = strings.ToLower(category)
	terms := strings.Fields(strings.ToLower(query))
	score := 0
	for _, term := range terms {
		term = strings.Trim(term, " ,.:;!?()[]{}\"'")
		if len(term) < 2 || term == "tool" || term == "use" || term == "need" || term == "want" {
			continue
		}
		for i, variant := range expandQueryVariants(term) {
			weight := 1
			if i == 0 {
				weight = 2
			}
			switch {
			case strings.Contains(name, variant):
				score += 6 * weight
			case strings.Contains(category, variant):
				score += 4 * weight
			case strings.Contains(description, variant):
				score += 2 * weight
			}
		}
	}
	if strings.Contains(name, strings.ReplaceAll(strings.ToLower(query), " ", "_")) {
		score += 20
	}
	return score
}

func liveToolCategory(name string) string {
	switch name {
	case "execute_command":
		return "terminal"
	case "read_file", "write_file", "edit_file", "grep_files":
		return "files"
	case "web_search", "browser":
		return "web"
	case "get_time_and_location":
		return "time and location"
	case "remember", "forget":
		return "memory"
	case "conversation_read":
		return "conversation history"
	case "schedule_reminder", "create_routine", "list_scheduled", "cancel_scheduled":
		return "scheduling"
	case "write_artifact", "create_slides":
		return "artifacts"
	case "connect_mcp":
		return "integrations"
	case "ask_user":
		return "interaction"
	default:
		if strings.Contains(name, "__") {
			return "integration"
		}
		return "other"
	}
}
