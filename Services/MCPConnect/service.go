package MCPConnect

import (
	"context"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/google/uuid"
)

const (
	Connect = "connect"
	Deny    = "deny"
)

type waitingRequest struct {
	ch        chan string
	AgentID   string
	AgentName string
	ServerID  string
	ServerName string
}

type Service struct {
	Core *Shared.Core

	mu      sync.Mutex
	waiting map[string]*waitingRequest
	byAgent map[string]string
}

func (s *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	if s.waiting == nil {
		s.waiting = map[string]*waitingRequest{}
	}
	if s.byAgent == nil {
		s.byAgent = map[string]string{}
	}
	return s.Core.Bootstrap(ctx)
}

func (s *Service) Respond(requestID, decision string) Shared.ActionResult {
	switch decision {
	case Connect, Deny:
	default:
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeBadInput, "Unknown decision")}
	}
	s.mu.Lock()
	w, ok := s.waiting[requestID]
	if ok {
		delete(s.waiting, requestID)
		if s.byAgent[w.AgentID] == requestID {
			delete(s.byAgent, w.AgentID)
		}
	}
	s.mu.Unlock()
	if !ok {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeNotFound, "That request is no longer active")}
	}
	s.Core.EmitEvent(Shared.EventMCPConnectResolved, Shared.MCPConnectDecisionView{
		RequestID: requestID,
		AgentID:   w.AgentID,
		Decision:  decision,
		ServerID:  w.ServerID,
	})
	w.ch <- decision
	return Shared.ActionResult{Ok: true}
}

func Request(ctx context.Context, svc *Service, agentID, agentName, serverID, serverName, description, icon string) (string, error) {
	svc.mu.Lock()
	if rid, ok := svc.byAgent[agentID]; ok {
		w := svc.waiting[rid]
		svc.mu.Unlock()
		return await(ctx, svc, w, rid, agentID)
	}
	rid, _ := uuid.NewV7()
	id := rid.String()
	w := &waitingRequest{
		ch:         make(chan string, 1),
		AgentID:    agentID,
		AgentName:  agentName,
		ServerID:   serverID,
		ServerName: serverName,
	}
	svc.waiting[id] = w
	svc.byAgent[agentID] = id
	svc.mu.Unlock()

	svc.Core.EmitEvent(Shared.EventMCPConnectRequested, Shared.MCPConnectRequestView{
		RequestID:   id,
		AgentID:     agentID,
		AgentName:   agentName,
		ServerID:    serverID,
		ServerName:  serverName,
		Description: description,
		Icon:        icon,
	})
	return await(ctx, svc, w, id, agentID)
}

func await(ctx context.Context, svc *Service, w *waitingRequest, rid, agentID string) (string, error) {
	select {
	case decision := <-w.ch:
		return decision, nil
	case <-ctx.Done():
		svc.mu.Lock()
		delete(svc.waiting, rid)
		if svc.byAgent[agentID] == rid {
			delete(svc.byAgent, agentID)
		}
		svc.mu.Unlock()
		return "", ctx.Err()
	case <-time.After(120 * time.Second):
		svc.mu.Lock()
		delete(svc.waiting, rid)
		if svc.byAgent[agentID] == rid {
			delete(svc.byAgent, agentID)
		}
		svc.mu.Unlock()
		return Deny, nil
	}
}
