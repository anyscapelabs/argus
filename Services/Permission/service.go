package Permission

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
	"github.com/google/uuid"
)

// TerminalTool is the single tool the permission gate currently guards.
const TerminalTool = "execute_command"

// Decision values the gate and the frontend agree on.
const (
	AllowOnce   = "allow_once"
	AllowAlways = "allow_always"
	Deny        = "deny"
)

type waitingRequest struct {
	ch chan string

	AgentID   string
	AgentName string
	Tool      string
	Command   string
}

type Service struct {
	Core *Shared.Core

	mu      sync.Mutex
	waiting map[string]*waitingRequest // requestID -> waiting
	byAgent map[string]string          // agentID -> requestID
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

// Respond resolves a pending permission request from the user. The decision
// values are "allow_once", "allow_always" or "deny". Allow Always persists a
// grant for the agent; the other two only unblock the current request.
func (s *Service) Respond(requestID, decision string) Shared.ActionResult {
	switch decision {
	case AllowOnce, AllowAlways, Deny:
	default:
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeBadInput, "Unknown permission decision")}
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
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeNotFound, "That permission request is no longer active")}
	}
	// Persist the decision as a notice so the UI can still show what was
	// granted after a restart.
	noticeID := ""
	if nid, uerr := uuid.NewV7(); uerr == nil {
		noticeID = nid.String()
	}
	if noticeID != "" {
		if serr := s.Core.DB.InsertPermissionNotice(Storage.PermissionNotice{
			ID:        noticeID,
			AgentID:   w.AgentID,
			AgentName: w.AgentName,
			Tool:      w.Tool,
			Command:   w.Command,
			Decision:  decision,
			CreatedAt: time.Now().UnixMilli(),
		}); serr != nil {
			log.Printf("permission: could not persist decision notice: %v", serr)
		}
	}
	s.Core.EmitEvent(Shared.EventPermissionResolved, Shared.PermissionDecisionView{
		RequestID: requestID,
		AgentID:   w.AgentID,
		AgentName: w.AgentName,
		Tool:      w.Tool,
		Command:   w.Command,
		Decision:  decision,
	})
	w.ch <- decision
	return Shared.ActionResult{Ok: true}
}

// Notices lists the persisted permission decisions of an agent, newest first.
func (s *Service) Notices(agentID string, limit int) Shared.NoticesResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.NoticesResult{Error: aerr}
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.Core.DB.ListPermissionNotices(agentID, limit)
	if err != nil {
		return Shared.NoticesResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	out := make([]Shared.PermissionNoticeView, 0, len(rows))
	for _, n := range rows {
		out = append(out, Shared.PermissionNoticeView{
			ID:        n.ID,
			AgentID:   n.AgentID,
			AgentName: n.AgentName,
			Tool:      n.Tool,
			Command:   n.Command,
			Decision:  n.Decision,
			CreatedAt: n.CreatedAt,
		})
	}
	return Shared.NoticesResult{Notices: out}
}

// State reports the persisted grant for an agent and the terminal tool.
func (s *Service) State(agentID string) Shared.PermissionStateResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.PermissionStateResult{Error: aerr}
	}
	grant, found, err := s.Core.DB.GetGrant(agentID, TerminalTool)
	if err != nil {
		return Shared.PermissionStateResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	state := Shared.PermissionState{AgentID: agentID, Tool: TerminalTool}
	if found {
		switch grant.Mode {
		case string(Storage.GrantAllow):
			state.Allowed = true
		case string(Storage.GrantDeny):
			state.Denied = true
		}
	}
	return Shared.PermissionStateResult{State: state}
}

// Revoke removes the persisted grant for an agent and the terminal tool.
func (s *Service) Revoke(agentID string) Shared.ActionResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.ActionResult{Error: aerr}
	}
	if err := s.Core.DB.ClearGrant(agentID, TerminalTool); err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	return Shared.ActionResult{Ok: true}
}

// Request is the gate entry point used by the agent loop. It returns the
// stored decision when a grant exists, otherwise it emits a
// permission:requested event and blocks until the user answers through
// Respond or the context is cancelled.
func Request(ctx context.Context, svc *Service, agentID, agentName, tool, command string) (string, error) {
	grant, found, err := svc.Core.DB.GetGrant(agentID, tool)
	if err != nil {
		return "", err
	}
	if found {
		switch grant.Mode {
		case string(Storage.GrantAllow):
			return AllowAlways, nil
		case string(Storage.GrantDeny):
			return Deny, nil
		}
	}

	// One pending ask per agent: if one is already on screen the caller
	// shares its outcome instead of stacking a second card.
	svc.mu.Lock()
	if rid, ok := svc.byAgent[agentID]; ok {
		w := svc.waiting[rid]
		svc.mu.Unlock()
		return await(ctx, svc, w, rid, agentID, tool, command, agentName)
	}
	rid, _ := uuid.NewV7()
	id := rid.String()
	w := &waitingRequest{
		ch:        make(chan string, 1),
		AgentID:   agentID,
		AgentName: agentName,
		Tool:      tool,
		Command:   command,
	}
	svc.waiting[id] = w
	svc.byAgent[agentID] = id
	svc.mu.Unlock()

	svc.Core.EmitEvent(Shared.EventPermissionRequested, Shared.PermissionRequestView{
		RequestID: id,
		AgentID:   agentID,
		AgentName: agentName,
		Tool:      tool,
		Command:   command,
	})
	return await(ctx, svc, w, id, agentID, tool, command, agentName)
}

func await(ctx context.Context, svc *Service, w *waitingRequest, rid, agentID, tool, command, agentName string) (string, error) {
	select {
	case decision := <-w.ch:
		if decision == AllowAlways {
			if err := svc.Core.DB.SetGrant(agentID, tool, Storage.GrantAllow); err != nil {
				return "", err
			}
		}
		return decision, nil
	case <-ctx.Done():
		svc.mu.Lock()
		delete(svc.waiting, rid)
		if svc.byAgent[agentID] == rid {
			delete(svc.byAgent, agentID)
		}
		svc.mu.Unlock()
		return "", ctx.Err()
	}
}
