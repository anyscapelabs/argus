// Package Skills implements 3-level progressive Agent Skills (SKILL.md)
// adapted to Argus's gateway tool model.
//
// Level 1 (Metadata): compact id + trigger description, either embedded in the
// app or refreshed from a signed index on the CDN, injected into the system
// prompt for installed skills and searchable with search_skills.
// Level 2 (Body): exact tool chains, query hints, and gotchas loaded via read_skill.
// Level 3 (References + Live Schemas): optional REFERENCE.md + unlocked MCP schemas.
package Skills

import (
	"context"
	"embed"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"

	"github.com/anyscapelabs/argus/Services/Shared"
)

//go:embed data/skills.json data/*/SKILL.md
var embeddedFS embed.FS

// DefaultIndexURL is the signed skills manifest published to CloudFront.
// ARGUS_SKILLS_INDEX_URL overrides it for staging via New.
const DefaultIndexURL = "https://registry.anyscapelabs.com/index.json"

type Service struct {
	Core *Shared.Core

	mu         sync.Mutex
	store      *Store
	buildErr   error
	indexURL   string
	cacheDir   string
	appVersion string
	cancel     context.CancelFunc
}

// New wires a skills service to a cache directory and signed index. Passing an
// empty index URL keeps it offline (embedded baseline only).
func New(core *Shared.Core, cacheDir, indexURL, appVersion string) (*Service, error) {
	s := &Service{Core: core, cacheDir: cacheDir, indexURL: indexURL, appVersion: appVersion}
	store, err := NewStore(cacheDir, indexURL, appVersion, func() {
		if core != nil {
			core.EmitEvent(Shared.EventSkillsUpdated, nil)
		}
	})
	if err != nil {
		return nil, err
	}
	s.store = store
	return s, nil
}

func (s *Service) ensureStore() *Store {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store != nil || s.buildErr != nil {
		return s.store
	}
	cacheDir := s.cacheDir
	if cacheDir == "" && s.Core != nil {
		if dir, err := Shared.CacheDir(); err == nil {
			cacheDir = dir
		}
	}
	indexURL := s.indexURL
	if indexURL == "" {
		indexURL = DefaultIndexURL
	}
	appVersion := s.appVersion
	if appVersion == "" {
		appVersion = "0.0.0"
	}
	store, err := NewStore(cacheDir, indexURL, appVersion, func() {
		if s.Core != nil {
			s.Core.EmitEvent(Shared.EventSkillsUpdated, nil)
		}
	})
	if err != nil {
		s.buildErr = err
		log.Printf("skills: registry unavailable: %v", err)
		return nil
	}
	s.store = store
	return store
}

// Start runs the background index refresh until ctx is cancelled.
func (s *Service) Start(ctx context.Context) {
	store := s.ensureStore()
	if store == nil {
		return
	}
	bg, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
	store.StartBackground(bg)
}

func (s *Service) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Service) Status() Status {
	if store := s.ensureStore(); store != nil {
		return store.Status()
	}
	return Status{Source: SourceEmbedded}
}

func (s *Service) All() []SkillMeta {
	store := s.ensureStore()
	if store == nil {
		return nil
	}
	return store.All()
}

func (s *Service) Get(id string) (SkillMeta, bool) {
	store := s.ensureStore()
	if store == nil {
		return SkillMeta{}, false
	}
	return store.Get(id)
}

// IsEnabled reports whether a skill is active for agentID. Built-in connector
// skills with AutoEnable=true activate automatically when their connector is
// connected or when explicitly enabled in SQLite.
func (s *Service) IsEnabled(agentID, skillID string, connectedServers map[string]bool) bool {
	meta, ok := s.Get(skillID)
	if !ok || meta.Revoked {
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

func (s *Service) Enable(agentID, skillID string) error {
	if _, ok := s.Get(skillID); !ok {
		return fmt.Errorf("unknown skill: %s", skillID)
	}
	if s.Core == nil || s.Core.DB == nil {
		return fmt.Errorf("storage not ready")
	}
	return s.Core.DB.SetAgentSkillState(agentID, skillID, true)
}

func (s *Service) Disable(agentID, skillID string) error {
	if s.Core == nil || s.Core.DB == nil {
		return fmt.Errorf("storage not ready")
	}
	return s.Core.DB.SetAgentSkillState(agentID, skillID, false)
}

func (s *Service) ReadSkill(agentID, skillID string) (SkillDoc, bool) {
	meta, ok := s.Get(skillID)
	if !ok || meta.Revoked {
		return SkillDoc{}, false
	}
	store := s.ensureStore()
	if store == nil {
		return SkillDoc{}, false
	}
	body, ok := store.ReadBody(skillID)
	if !ok {
		return SkillDoc{}, false
	}
	return SkillDoc{Meta: meta, Body: strings.TrimSpace(body)}, true
}

// Install downloads and verifies a skill's bundle before enabling it for an
// agent. Embedded skills have no remote bundle and are enabled straight away.
func (s *Service) Install(agentID, skillID string) error {
	meta, ok := s.Get(skillID)
	if !ok {
		return fmt.Errorf("unknown skill: %s", skillID)
	}
	if meta.Revoked {
		if meta.ReplacedBy != "" {
			return fmt.Errorf("skill %s is revoked; use %s", skillID, meta.ReplacedBy)
		}
		return fmt.Errorf("skill %s is revoked", skillID)
	}
	if store := s.ensureStore(); store != nil {
		if err := store.FetchBundle(context.Background(), skillID); err != nil {
			return err
		}
	}
	return s.Enable(agentID, skillID)
}

// Search ranks the full registry by id/name/description matches so the system
// prompt only has to carry installed skills' one-liners.
func (s *Service) Search(query string, limit int) []SkillMeta {
	terms := strings.Fields(strings.ToLower(strings.TrimSpace(query)))
	if limit <= 0 {
		limit = 8
	}
	if limit > 20 {
		limit = 20
	}
	type hit struct {
		meta  SkillMeta
		score int
	}
	var hits []hit
	for _, m := range s.All() {
		if m.Revoked {
			continue
		}
		id, name, desc := strings.ToLower(m.ID), strings.ToLower(m.Name), strings.ToLower(m.Description)
		score := 0
		if len(terms) == 0 {
			score = 1
		}
		for _, term := range terms {
			if id == term {
				score += 100
			} else if strings.Contains(id, term) {
				score += 20
			}
			if strings.Contains(name, term) {
				score += 10
			}
			if strings.Contains(desc, term) {
				score += 5
			}
		}
		if score > 0 {
			hits = append(hits, hit{m, score})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].meta.ID < hits[j].meta.ID
	})
	out := make([]SkillMeta, 0, min(limit, len(hits)))
	for i := 0; i < limit && i < len(hits); i++ {
		out = append(out, hits[i].meta)
	}
	return out
}

// PromptBlock renders Level 1 skill summaries for the system prompt. Only
// installed skills are listed; discovery moves to the search_skills tool so
// the standing prompt does not grow with the registry.
func (s *Service) PromptBlock(agentID string, connectedServers map[string]bool) string {
	var active []SkillMeta
	for _, m := range s.All() {
		if s.IsEnabled(agentID, m.ID, connectedServers) {
			active = append(active, m)
		}
	}
	sort.Slice(active, func(i, j int) bool { return active[i].ID < active[j].ID })

	var b strings.Builder
	if len(active) > 0 {
		b.WriteString("Installed skills (call `read_skill(name)` FIRST when a task matches):\n")
		for _, m := range active {
			fmt.Fprintf(&b, "- %s: %s\n", m.ID, m.Description)
		}
	}
	b.WriteString("Call `search_skills(query)` to discover other available skills.")
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
