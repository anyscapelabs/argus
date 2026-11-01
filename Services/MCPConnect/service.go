package MCPConnect

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/anyscapelabs/argus/Services/Integrations"
	"github.com/anyscapelabs/argus/Services/MCP"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/google/uuid"
)

const requestTTL = 10 * time.Minute

type request struct {
	view   Shared.MCPConnectRequestView
	cancel context.CancelFunc
	expire *time.Timer
}

type Service struct {
	Core     *Shared.Core
	MCP      *MCP.Service
	Catalog  *Integrations.Catalog
	mu       sync.Mutex
	reqs     map[string]*request
	byAgent  map[string]string
}

func (s *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	if s.reqs == nil {
		s.reqs = map[string]*request{}
	}
	if s.byAgent == nil {
		s.byAgent = map[string]string{}
	}
	if s.Catalog == nil {
		if cat, err := Integrations.Default(); err == nil {
			s.Catalog = cat
		}
	}
	return s.Core.Bootstrap(ctx)
}

func (s *Service) emit(r *request, state, msg string) {
	toolCount := 0
	if s.MCP != nil {
		toolCount = len(s.MCP.ListTools())
	}
	s.Core.EmitEvent(Shared.EventMCPConnectStatus, map[string]any{
		"request_id": r.view.RequestID,
		"agent_id":   r.view.AgentID,
		"server_id":  r.view.ServerID,
		"server_name": r.view.ServerName,
		"state":       state,
		"error":       msg,
		"tool_count":  toolCount,
	})
}

// Register returns shown | pending | connected | unknown.
func (s *Service) Register(agentID, agentName, serverID string) string {
	if s.Catalog == nil {
		if cat, err := Integrations.Default(); err == nil {
			s.Catalog = cat
		} else {
			return "unknown"
		}
	}
	it, ok := s.Catalog.Get(serverID)
	if !ok {
		return "unknown"
	}
	if s.MCP != nil && s.MCP.Manager().IsConnected(serverID) {
		return "connected"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, busy := s.byAgent[agentID]; busy {
		return "pending"
	}
	id, _ := uuid.NewV7()
	r := &request{
		view: Shared.MCPConnectRequestView{
			RequestID:   id.String(),
			AgentID:     agentID,
			AgentName:   agentName,
			ServerID:    it.ID,
			ServerName:  it.Name,
			Description: it.Description,
			Icon:        it.Icon,
		},
	}
	r.expire = time.AfterFunc(requestTTL, func() { s.finish(r.view.RequestID, "expired", "") })
	s.reqs[r.view.RequestID] = r
	s.byAgent[agentID] = r.view.RequestID
	s.emit(r, "requested", "")
	return "shown"
}

func (s *Service) Respond(requestID, decision string) Shared.ActionResult {
	s.mu.Lock()
	r, ok := s.reqs[requestID]
	s.mu.Unlock()
	if !ok {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeNotFound, "That request is no longer active")}
	}
	switch decision {
	case "deny":
		s.finish(requestID, "denied", "")
	case "cancel":
		s.mu.Lock()
		if r.cancel != nil {
			r.cancel()
		}
		s.mu.Unlock()
	case "connect":
		s.mu.Lock()
		if r.cancel != nil {
			s.mu.Unlock()
			return Shared.ActionResult{Ok: true}
		}
		ctx, cancel := context.WithCancel(context.Background())
		r.cancel = cancel
		r.expire.Stop()
		s.mu.Unlock()
		s.emit(r, "authorizing", "")
		go s.runConnect(ctx, r)
	default:
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeBadInput, "Unknown decision")}
	}
	return Shared.ActionResult{Ok: true}
}

func (s *Service) runConnect(ctx context.Context, r *request) {
	if s.MCP == nil {
		s.retryable(r, "MCP service not available")
		return
	}
	err := s.MCP.ConnectRemote(ctx, r.view.ServerID, true)
	switch {
	case err == nil:
		s.finish(r.view.RequestID, "connected", "")
	case errors.Is(err, context.Canceled):
		s.retryable(r, "Sign-in cancelled")
	default:
		s.retryable(r, err.Error())
	}
}

func (s *Service) retryable(r *request, msg string) {
	s.mu.Lock()
	r.cancel = nil
	r.expire.Reset(requestTTL)
	s.mu.Unlock()
	s.emit(r, "failed", msg)
}

func (s *Service) finish(id, state, msg string) {
	s.mu.Lock()
	r, ok := s.reqs[id]
	if ok {
		delete(s.reqs, id)
		if r.view.AgentID != "" {
			if s.byAgent[r.view.AgentID] == id {
				delete(s.byAgent, r.view.AgentID)
			}
		}
		r.expire.Stop()
	}
	s.mu.Unlock()
	if ok {
		s.emit(r, state, msg)
	}
}

// Pending lets the frontend restore the card after a remount or reload.
func (s *Service) Pending(agentID string) []Shared.MCPConnectRequestView {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Shared.MCPConnectRequestView
	for _, r := range s.reqs {
		if r.view.AgentID == agentID {
			out = append(out, r.view)
		}
	}
	return out
}
