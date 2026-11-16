package Shared

import (
	"log"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/anyscapelabs/argus/Storage"
)

// InteractionStore is the one-request bookkeeper behind interaction cards: an
// app-side request (a browser handover, a connector sign-in) that the UI's
// buttons answer and the backend settles. Both cards need the same mechanics —
// a map by request id, a per-agent owner index for "one live card per agent",
// lazy init, removal on settlement, and a settled row for restores — so it
// lives here once: correctness stops depending on each service remembering its
// own version. The domain logic (what a request carries, what settling means,
// what the notice and the steer say) stays with each service.
//
// The store holds V (a pointer to the service's request struct, or the view
// itself); it never owns what V contains.
type InteractionStore[V any] struct {
	mu      sync.Mutex
	byID    map[string]V
	byAgent map[string]string
}

// Begin parks a fresh request: it fails when the agent already has a live
// request registered, so a second raise returns the existing card instead of
// stacking a second ask.
func (s *InteractionStore[V]) Begin(agentID string, v V, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byAgent == nil {
		s.byAgent = map[string]string{}
	}
	if s.byID == nil {
		s.byID = map[string]V{}
	}
	if _, busy := s.byAgent[agentID]; busy {
		return false
	}
	s.byID[id] = v
	s.byAgent[agentID] = id
	return true
}

// Get returns the request by id.
func (s *InteractionStore[V]) Get(id string) (V, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.byID[id]
	return v, ok
}

// Live returns the agent's live request, if any.
func (s *InteractionStore[V]) Live(agentID string) (V, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byAgent[agentID]
	if !ok {
		var zero V
		return zero, false
	}
	v, ok := s.byID[id]
	return v, ok
}

// List returns the agent's live requests (at most one, per the one-live-card
// rule).
func (s *InteractionStore[V]) List(agentID string) []V {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byAgent[agentID]
	if !ok {
		return nil
	}
	if v, ok := s.byID[id]; ok {
		return []V{v}
	}
	return nil
}

// End unregisters a request from both indexes.
func (s *InteractionStore[V]) End(id string, agentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byID, id)
	if s.byAgent[agentID] == id {
		delete(s.byAgent, agentID)
	}
}

// Lock gives the service its domain-lock for work that needs the request's
// interior (a sign-in's cancel handle, an expiring timer) under the store's
// mutex. Use sparingly; the request struct's fields are the service's care.
func (s *InteractionStore[V]) Lock()   { s.mu.Lock() }
func (s *InteractionStore[V]) Unlock() { s.mu.Unlock() }

// Swap replaces the request before a state change that re-creates it (a
// sign-in restart replacing its cancel handle). Present mainly to keep one
// map, not three.
func (s *InteractionStore[V]) Swap(id string, v V) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[id] = v
}

func (s *InteractionStore[V]) listLocked(_ string) []V { return nil }

// PersistNotice writes the settled state of an interaction into the notices
// table. kind is the row's wardrobe ("mcp", "takeover", "permission") and body
// the finished sentence, because a decision word alone cannot describe what
// happened after a reload or a restart.
func PersistNotice(core *Core, agentID, agentName, tool, decision, kind, body string) {
	if core == nil || core.DB == nil || agentID == "" {
		return
	}
	id, err := uuid.NewV7()
	if err != nil {
		return
	}
	if err := core.DB.InsertPermissionNotice(Storage.PermissionNotice{
		ID:        id.String(),
		AgentID:   agentID,
		AgentName: agentName,
		Tool:      tool,
		Decision:  decision,
		Kind:      kind,
		Body:      body,
		CreatedAt: time.Now().UnixMilli(),
	}); err != nil {
		log.Printf("interaction: could not persist %s notice: %v", decision, err)
	}
}
