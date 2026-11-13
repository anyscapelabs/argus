package Chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Services/Permission"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Services/Skills"
)

func gatewayToolDefs() []Protocol.ToolDef {
	return []Protocol.ToolDef{
		conversationReadToolDef,
		webSearchToolDef,
		liveSearchToolsDef,
		liveInvokeToolDef,
		liveReadSkillDef,
		liveInstallSkillDef,
	}
}

func isGatewayMetaTool(name string) bool {
	switch name {
	case liveSearchToolsDef.Name, liveInvokeToolDef.Name, liveReadSkillDef.Name, liveInstallSkillDef.Name:
		return true
	default:
		return false
	}
}

// runTypedGatewayTool executes one of the two meta-tools for a normal chat run.
// The outer id/name must be preserved so provider tool-call pairing remains
// valid even though the backend executes an inner tool with its real name.
func (s *Service) runTypedGatewayTool(
	ctx context.Context,
	runID, agentID, agentName string,
	discovered map[string]bool,
	outer Protocol.ToolCall,
) (Protocol.Block, string) {
	result := func(text string, isError bool) Protocol.Block {
		return Protocol.Block{
			Type: Protocol.BlockToolResult, ToolRefID: outer.ID,
			ToolName: outer.Name, Text: text, IsError: isError,
		}
	}
	if !outer.Valid {
		return result(invalidCallMessage(outer.Name), true), ""
	}
	if outer.Name == liveSearchToolsDef.Name {
		found := s.searchAvailableTools(discovered, Shared.IsScheduledFire(ctx), !Shared.IsUnattended(ctx), outer.Input)
		if found.Error != nil {
			return result(found.Error.Message, true), ""
		}
		return result(found.Output, found.IsError), ""
	}
	if outer.Name == liveReadSkillDef.Name {
		res := s.readSkillGateway(agentID, discovered, outer.Input)
		return result(res.Output, res.IsError), ""
	}
	if outer.Name == liveInstallSkillDef.Name {
		res := s.installSkillGateway(ctx, agentID, agentName, outer.Input)
		return result(res.Output, res.IsError), ""
	}
	if outer.Name != liveInvokeToolDef.Name {
		return result("unknown gateway tool: "+outer.Name, true), ""
	}
	var input struct {
		Name          string `json:"name"`
		ArgumentsJSON string `json:"arguments_json"`
	}
	if err := json.Unmarshal([]byte(outer.Input), &input); err != nil {
		return result("invalid invoke_tool arguments", true), ""
	}
	input.Name = strings.TrimSpace(input.Name)
	if !discovered[input.Name] {
		return result("tool was not returned by search_tools in this run: "+input.Name, true), input.Name
	}
	arguments := strings.TrimSpace(input.ArgumentsJSON)
	if arguments == "" {
		arguments = "{}"
	}
	inner := s.runTool(ctx, runID, agentID, agentName, Protocol.ToolCall{
		ID: outer.ID, Name: input.Name, Input: arguments, Valid: json.Valid([]byte(arguments)),
	})
	inner.ToolRefID = outer.ID
	inner.ToolName = outer.Name
	return inner, input.Name
}

func (s *Service) skillsRegistry() *Skills.Service {
	if s.Skills != nil {
		return s.Skills
	}
	if s.Core != nil {
		s.Skills = &Skills.Service{Core: s.Core}
		return s.Skills
	}
	return nil
}

func (s *Service) skillsPromptBlock(agentID string, withTools bool) string {
	if !withTools {
		return ""
	}
	reg := s.skillsRegistry()
	if reg == nil {
		return ""
	}
	return reg.PromptBlock(agentID, s.connectedServerSet())
}

// readSkillGateway loads Level-2 SKILL.md instructions and unlocks the skill's
// declared tools + live MCP schemas into `discovered` so weak models can call
// invoke_tool directly without an extra search_tools round-trip.
func (s *Service) readSkillGateway(agentID string, discovered map[string]bool, argsJSON string) Shared.LiveToolResult {
	reg := s.skillsRegistry()
	if reg == nil {
		return Shared.LiveToolResult{Output: "skills service unavailable", IsError: true}
	}
	var input struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return Shared.LiveToolResult{Output: "invalid read_skill arguments", IsError: true}
	}
	id := strings.TrimSpace(input.Name)
	if id == "" {
		return Shared.LiveToolResult{Output: "read_skill requires a skill name", IsError: true}
	}
	doc, ok := reg.ReadSkill(agentID, id)
	if !ok {
		return Shared.LiveToolResult{
			Output:  "skill not found or not enabled: " + id + ". Check Installed skills or call install_skill first.",
			IsError: true,
		}
	}

	var b strings.Builder
	b.WriteString(doc.Body)

	// Attach live schemas for every tool in this skill's connector(s) or
	// explicit allowed_tools list, and mark them discovered for this turn.
	var unlocked []Protocol.ToolDef
	allowedSet := map[string]bool{}
	for _, t := range doc.Meta.AllowedTools {
		allowedSet[t] = true
	}
	connSet := map[string]bool{}
	for _, c := range doc.Meta.Connectors {
		connSet[c] = true
	}
	for _, def := range s.liveToolCatalog(false, true) {
		if allowedSet[def.Name] {
			unlocked = append(unlocked, def)
			discovered[def.Name] = true
			continue
		}
		for c := range connSet {
			if strings.HasPrefix(def.Name, mcpServerPrefix(c)) {
				unlocked = append(unlocked, def)
				discovered[def.Name] = true
				break
			}
		}
	}
	if len(unlocked) > 0 {
		b.WriteString("\n\n## Unlocked Live Tool Schemas (ready for invoke_tool)\n")
		for _, def := range unlocked {
			fmt.Fprintf(&b, "- `%s`: %s\n  input_schema: %s\n", def.Name, def.Description, string(def.Schema))
		}
	} else if len(doc.Meta.Connectors) > 0 {
		fmt.Fprintf(&b, "\n\nNote: Connector(s) [%s] are not currently connected. Call connect_mcp first if needed.\n", strings.Join(doc.Meta.Connectors, ", "))
	}
	return Shared.LiveToolResult{Output: b.String()}
}

func (s *Service) installSkillGateway(ctx context.Context, agentID, agentName, argsJSON string) Shared.LiveToolResult {
	reg := s.skillsRegistry()
	if reg == nil {
		return Shared.LiveToolResult{Output: "skills service unavailable", IsError: true}
	}
	var input struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return Shared.LiveToolResult{Output: "invalid install_skill arguments", IsError: true}
	}
	id := strings.TrimSpace(input.Name)
	meta, ok := reg.Get(id)
	if !ok {
		return Shared.LiveToolResult{Output: "unknown skill: " + id, IsError: true}
	}
	if s.Permissions != nil {
		decision, err := Permission.Request(ctx, s.Permissions, agentID, agentName, Permission.SkillInstallTool, meta.ID+" ("+meta.Name+")")
		if err != nil {
			return Shared.LiveToolResult{Output: "permission check failed: " + err.Error(), IsError: true}
		}
		if decision == Permission.Deny {
			return Shared.LiveToolResult{Output: "user denied installing skill " + meta.ID, IsError: true}
		}
	}
	if err := reg.Enable(agentID, meta.ID); err != nil {
		return Shared.LiveToolResult{Output: "could not enable skill: " + err.Error(), IsError: true}
	}
	return Shared.LiveToolResult{Output: "Skill \"" + meta.ID + "\" installed and enabled. Call read_skill(\"" + meta.ID + "\") to load its instructions."}
}
