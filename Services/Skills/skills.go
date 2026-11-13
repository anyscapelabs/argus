// Package Skills implements 3-level progressive Agent Skills (SKILL.md)
// adapted to Argus's gateway tool model.
//
// Level 1 (Metadata): compact id + trigger description injected into system prompt.
// Level 2 (Body): exact tool chains, query hints, and gotchas loaded via read_skill.
// Level 3 (References + Live Schemas): optional REFERENCE.md + unlocked MCP schemas.
package Skills

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/anyscapelabs/argus/Services/Shared"
)

//go:embed data/skills.json data/*/SKILL.md
var embeddedFS embed.FS

// SkillMeta is Level 1 metadata loaded from skills.json / YAML frontmatter.
type SkillMeta struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Connectors   []string `json:"connectors"`
	AllowedTools []string `json:"allowed_tools,omitempty"`
	AutoEnable   bool     `json:"auto_enable"`
}

// SkillDoc is Level 2 content returned when read_skill is called.
type SkillDoc struct {
	Meta SkillMeta `json:"meta"`
	Body string    `json:"body"`
}

// Service manages built-in and agent-enabled skills.
type Service struct {
	Core   *Shared.Core
	loaded bool
	metas  []SkillMeta
	byID   map[string]SkillMeta
}

func (s *Service) ensureLoaded() {
	if s.loaded {
		return
	}
	s.loaded = true
	s.byID = map[string]SkillMeta{}
	raw, err := embeddedFS.ReadFile("data/skills.json")
	if err != nil {
		return
	}
	var list []SkillMeta
	if err := json.Unmarshal(raw, &list); err != nil {
		return
	}
	s.metas = list
	for _, m := range list {
		s.byID[m.ID] = m
	}
}

// All returns all registered skill metadata entries.
func (s *Service) All() []SkillMeta {
	s.ensureLoaded()
	out := make([]SkillMeta, len(s.metas))
	copy(out, s.metas)
	return out
}

// Get looks up a skill by id.
func (s *Service) Get(id string) (SkillMeta, bool) {
	s.ensureLoaded()
	m, ok := s.byID[strings.TrimSpace(id)]
	return m, ok
}

// IsEnabled reports whether a skill is active for agentID. Built-in connector
// skills with AutoEnable=true activate automatically when their connector is
// connected or when explicitly enabled in SQLite.
func (s *Service) IsEnabled(agentID, skillID string, connectedServers map[string]bool) bool {
	meta, ok := s.Get(skillID)
	if !ok {
		return false
	}
	if s.Core != nil && s.Core.DB != nil {
		if state, found, err := s.Core.DB.GetAgentSkillState(agentID, skillID); err == nil && found {
			return state
		}
	}
	if meta.AutoEnable {
		if len(meta.Connectors) == 0 {
			return true
		}
		for _, c := range meta.Connectors {
			if connectedServers[c] {
				return true
			}
		}
	}
	return false
}

// Enable persists an explicit enable toggle for an agent skill.
func (s *Service) Enable(agentID, skillID string) error {
	if _, ok := s.Get(skillID); !ok {
		return fmt.Errorf("unknown skill: %s", skillID)
	}
	if s.Core == nil || s.Core.DB == nil {
		return fmt.Errorf("storage not ready")
	}
	return s.Core.DB.SetAgentSkillState(agentID, skillID, true)
}

// Disable persists an explicit disable toggle for an agent skill.
func (s *Service) Disable(agentID, skillID string) error {
	if s.Core == nil || s.Core.DB == nil {
		return fmt.Errorf("storage not ready")
	}
	return s.Core.DB.SetAgentSkillState(agentID, skillID, false)
}

// ReadSkill returns Level 2 SKILL.md body (with YAML frontmatter stripped).
func (s *Service) ReadSkill(agentID, skillID string) (SkillDoc, bool) {
	meta, ok := s.Get(skillID)
	if !ok {
		return SkillDoc{}, false
	}
	p := path.Join("data", meta.ID, "SKILL.md")
	raw, err := fs.ReadFile(embeddedFS, p)
	if err != nil {
		return SkillDoc{}, false
	}
	body := stripYAMLFrontmatter(string(raw))
	return SkillDoc{Meta: meta, Body: strings.TrimSpace(body)}, true
}

// PromptBlock renders Level 1 skill summaries for the system prompt.
// Only skills whose connectors are connected (or explicitly installed) appear
// under "Installed skills", keeping standing token overhead ~35 tokens/skill.
func (s *Service) PromptBlock(agentID string, connectedServers map[string]bool) string {
	s.ensureLoaded()
	if len(s.metas) == 0 {
		return ""
	}
	var active []SkillMeta
	var available []string
	for _, m := range s.metas {
		if s.IsEnabled(agentID, m.ID, connectedServers) {
			active = append(active, m)
		} else {
			available = append(available, m.ID)
		}
	}
	sort.Slice(active, func(i, j int) bool { return active[i].ID < active[j].ID })

	var b strings.Builder
	b.WriteString("\n## Agent Skills\n")
	if len(active) > 0 {
		b.WriteString("Installed skills (call `read_skill(name)` FIRST when a task matches):\n")
		for _, m := range active {
			fmt.Fprintf(&b, "- %s: %s\n", m.ID, m.Description)
		}
	}
	if len(available) > 0 {
		fmt.Fprintf(&b, "Available skills (require `install_skill(name)` if needed): %s\n", strings.Join(available, ", "))
	}
	return strings.TrimRight(b.String(), "\n")
}

func stripYAMLFrontmatter(md string) string {
	md = strings.TrimLeft(md, "\ufeff")
	if !strings.HasPrefix(md, "---\n") && !strings.HasPrefix(md, "---\r\n") {
		return md
	}
	rest := md[4:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return md
	}
	after := rest[idx+4:]
	after = strings.TrimPrefix(after, "\r")
	after = strings.TrimPrefix(after, "\n")
	return after
}
