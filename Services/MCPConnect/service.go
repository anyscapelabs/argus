package MCPConnect

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/anyscapelabs/argus/Services/Integrations"
	"github.com/anyscapelabs/argus/Services/MCP"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
	"github.com/google/uuid"
)

const requestTTL = 10 * time.Minute

type request struct {
	view   Shared.MCPConnectRequestView
	cancel context.CancelFunc
	expire *time.Timer
}

type Service struct {
	Core    *Shared.Core
	MCP     *MCP.Service
	Catalog *Integrations.Catalog
	// OnSettled hands a finished sign-in back to the agent as a steering turn.
	// It is a plain function field, set in main.go, because Chat already
	// imports this package — calling Chat directly would be an import cycle.
	// The shape mirrors Scheduler's narrow chat runner: this service says what
	// happened, something else decides how to run.
	OnSettled func(agentID, text string)
	mu        sync.Mutex
	reqs      map[string]*request
	byAgent   map[string]string
	// last holds the card that settled per agent, so a reload can still show
	// which integration is signed in rather than starting from nothing.
	last map[string]Shared.MCPConnectRequestView
}

func (s *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	if s.reqs == nil {
		s.reqs = map[string]*request{}
	}
	if s.byAgent == nil {
		s.byAgent = map[string]string{}
	}
	if s.last == nil {
		s.last = map[string]Shared.MCPConnectRequestView{}
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
		"request_id":  r.view.RequestID,
		"agent_id":    r.view.AgentID,
		"server_id":   r.view.ServerID,
		"server_name": r.view.ServerName,
		"description": r.view.Description,
		"icon":        r.view.Icon,
		"created_at":  r.view.CreatedAt,
		"state":       state,
		"error":       msg,
		"tool_count":  toolCount,
	})
}

// catalog returns the integration catalog, loading it on first use so a card
// can be built even if this service starts before the catalog is wired.
func (s *Service) catalog() *Integrations.Catalog {
	if s.Catalog == nil {
		if cat, err := Integrations.Default(); err == nil {
			s.Catalog = cat
		}
	}
	return s.Catalog
}

// ServerIDs lists the integrations the agent is allowed to ask for, read from
// the catalog so its tool description can never fall behind the real list.
func (s *Service) ServerIDs() []string {
	cat := s.catalog()
	if cat == nil {
		return nil
	}
	return cat.IDs()
}

// PromptBlock says which connectors are live at this very moment. The chat
// rebuilds it into the system prompt on every run, so a sign-in that finished
// after the agent last looked is already in front of it: the user never has to
// announce that they connected. The tool names are spelled out because the
// model otherwise has to guess how a connector's tools are written.
func (s *Service) PromptBlock(withTools bool) string {
	cat := s.catalog()
	if cat == nil || s.MCP == nil {
		return ""
	}
	tools := map[string]int{}
	for _, t := range s.MCP.ListTools() {
		tools[t.ServerID]++
	}
	var live, idle []string
	for _, it := range cat.Items() {
		if it.URL == "" {
			continue
		}
		switch {
		case tools[it.ID] > 0:
			live = append(live, fmt.Sprintf("- %s (id %s): connected, %d tools", it.Name, it.ID, tools[it.ID]))
		case s.MCP.Manager() != nil && s.MCP.Manager().IsConnected(it.ID):
			// Signed in but published no tools: still a session, not a prompt.
			live = append(live, fmt.Sprintf("- %s (id %s): connected", it.Name, it.ID))
		default:
			idle = append(idle, it.Name)
		}
	}
	if len(live) == 0 && len(idle) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Integrations\n")
	if len(live) > 0 {
		b.WriteString("Connected now:\n")
		for _, l := range live {
			b.WriteString(l + "\n")
		}
		if withTools {
			b.WriteString("Their tools are NOT attached to this conversation by default. When a request needs one of them, call load_mcp_tools with the server_id first; the tool schemas arrive with your next turn and are named <server_id>__<tool_name>.\n")
		}
		b.WriteString("This list is current as of this message. If the user signed into one of these after you last looked, it is connected: carry on, and do not ask them to confirm it or call connect_mcp again.\n")
	}
	if len(idle) > 0 {
		fmt.Fprintf(&b, "Not connected: %s.\n", strings.Join(idle, ", "))
		if withTools {
			b.WriteString("Use connect_mcp with the server_id when the user needs one of these.\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// toolPrefix is the server part of a connector tool name. Chat owns the full
// spelling and truncates at the provider's 64-character limit; the id alone
// never reaches that, so the replacement here is enough for the model to
// recognise a name it is about to be told about.
func toolPrefix(serverID string) string {
	return strings.NewReplacer("-", "_", ".", "_").Replace(serverID)
}

// Register returns shown | pending | connected | unknown.
func (s *Service) Register(agentID, agentName, serverID string) string {
	cat := s.catalog()
	if cat == nil {
		return "unknown"
	}
	it, ok := cat.Get(serverID)
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
			// The instant the card joins the conversation. The frontend weaves it
			// into the transcript here, so it stays at the point the agent asked
			// for the sign-in rather than following newer messages to the bottom.
			CreatedAt: time.Now().UnixMilli(),
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
	s.persistNotice(r.view, "failed", msg)
	s.emit(r, "failed", msg)
}

func (s *Service) finish(id, state, msg string) {
	s.mu.Lock()
	var cancel context.CancelFunc
	r, ok := s.reqs[id]
	if ok {
		delete(s.reqs, id)
		if r.view.AgentID != "" {
			if s.byAgent[r.view.AgentID] == id {
				delete(s.byAgent, r.view.AgentID)
			}
		}
		r.expire.Stop()
		cancel = r.cancel
		if state == "connected" {
			// The card outlives the request that produced it: it is the only
			// place that says this integration is signed in.
			done := r.view
			done.State = state
			s.last[r.view.AgentID] = done
		}
	}
	s.mu.Unlock()
	// An expired or denied card must stop the sign-in that is still waiting on
	// it, otherwise the loopback listener and its browser tab outlive the card
	// that opened them. A completed connect has already detached its context,
	// so cancelling then is a no-op and cannot close the live session.
	if cancel != nil {
		cancel()
	}
	if ok {
		s.persistNotice(r.view, state, msg)
		s.emit(r, state, msg)
		s.steer(r.view, state)
	}
}

// steer tells the agent how the sign-in ended, in a turn it reads but the user
// never sees. Without it the connection lands nowhere in the model's history:
// the run that asked for it already finished, and the next run would only pick
// up the new session by luck through the integrations block. The connect copy
// also needs the server id, which is what the tool names are built from.
//
// Two settlements are left alone. A failed attempt is still on screen and
// retryable, so steering would only make the agent talk about an error the user
// can fix in one click. An expired card usually means nobody was there — the
// timer runs whether or not the window is focused, and starting an unrequested
// run ten minutes after someone walked away is worse than the agent learning
// "not connected" from the next integrations block.
func (s *Service) steer(v Shared.MCPConnectRequestView, state string) {
	if s.OnSettled == nil || v.AgentID == "" {
		return
	}
	text := s.steerText(v, state)
	if text == "" {
		return
	}
	s.OnSettled(v.AgentID, text)
}

func (s *Service) steerText(v Shared.MCPConnectRequestView, state string) string {
	name := v.ServerName
	if name == "" {
		name = v.ServerID
	}
	switch state {
	case "connected":
		tools := 0
		if s.MCP != nil {
			for _, t := range s.MCP.ListTools() {
				if t.ServerID == v.ServerID {
					tools++
				}
			}
		}
		if tools > 0 {
			return fmt.Sprintf("(%s just finished connecting. Its tools are available now, named %s__<tool_name>. Pick up where you stopped — do not ask the user to confirm it and do not call connect_mcp for it again.)", name, toolPrefix(v.ServerID))
		}
		return fmt.Sprintf("(%s just finished connecting. It published no tools, so treat it as a session only.)", name)
	case "denied":
		return fmt.Sprintf("(%s declined the %s connection. Continue without it and do not ask again in this turn.)", name, v.ServerID)
	default:
		return ""
	}
}

// noticeTextFor is the line the conversation keeps once a card settles. The
// card leaves the pending list when it succeeds or gives up, so this row is the
// only thing that still says what happened after a reload or a restart.
func noticeTextFor(v Shared.MCPConnectRequestView, state, msg string) string {
	name := v.ServerName
	if name == "" {
		name = "The integration"
	}
	switch state {
	case "connected":
		return name + " connected"
	case "failed":
		if msg == "Sign-in cancelled" {
			return name + " sign-in cancelled"
		}
		if msg == "" {
			msg = "unknown error"
		}
		return name + " could not connect: " + msg
	case "denied":
		return name + " connection cancelled"
	case "expired":
		return name + " connection request expired"
	default:
		return name + " " + state
	}
}

// persistNotice writes the settled state into the notices table. Kind is "mcp"
// so the row is not mistaken for a terminal permission, and Body carries the
// finished sentence because a decision word alone cannot describe a sign-in.
func (s *Service) persistNotice(v Shared.MCPConnectRequestView, state, msg string) {
	if s.Core == nil || s.Core.DB == nil || v.AgentID == "" {
		return
	}
	id, err := uuid.NewV7()
	if err != nil {
		return
	}
	err = s.Core.DB.InsertPermissionNotice(Storage.PermissionNotice{
		ID:        id.String(),
		AgentID:   v.AgentID,
		AgentName: v.AgentName,
		Tool:      v.ServerID,
		Decision:  state,
		Kind:      "mcp",
		Body:      noticeTextFor(v, state, msg),
		CreatedAt: time.Now().UnixMilli(),
	})
	if err != nil {
		log.Printf("mcpconnect: could not persist %s notice: %v", state, err)
	}
}

// settledFromStore rebuilds the connected card from the notices table, which is
// what lets it survive an app restart. RequestID stays empty because a settled
// card has no buttons and nothing left to respond to. The keyring is asked
// rather than the live manager: session restore runs in the background, and a
// card that vanished because it was queried a second too early would look like
// the sign-in had been forgotten.
func (s *Service) settledFromStore(agentID string) (Shared.MCPConnectRequestView, bool) {
	if s.Core == nil || s.Core.DB == nil || s.MCP == nil {
		return Shared.MCPConnectRequestView{}, false
	}
	n, ok, err := s.Core.DB.LatestNoticeOf(agentID, "mcp", "connected")
	if err != nil || !ok || !s.MCP.HasStoredSession(n.Tool) {
		return Shared.MCPConnectRequestView{}, false
	}
	cat := s.catalog()
	if cat == nil {
		return Shared.MCPConnectRequestView{}, false
	}
	it, found := cat.Get(n.Tool)
	if !found {
		return Shared.MCPConnectRequestView{}, false
	}
	return Shared.MCPConnectRequestView{
		AgentID:     agentID,
		AgentName:   n.AgentName,
		ServerID:    it.ID,
		ServerName:  it.Name,
		Description: it.Description,
		Icon:        it.Icon,
		State:       "connected",
		// The settled notice is the only surviving stamp, so the restored card
		// anchors where the sign-in landed. It keeps its place in the transcript
		// from there, which is what the notice beside it does too.
		CreatedAt: n.CreatedAt,
	}, true
}

// Pending lets the frontend restore the card after a remount or reload. An
// unanswered request comes first, because that is the one needing a click; the
// card that settled last follows so a reload still shows what is connected.
func (s *Service) Pending(agentID string) []Shared.MCPConnectRequestView {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Shared.MCPConnectRequestView
	for _, r := range s.reqs {
		if r.view.AgentID != agentID {
			continue
		}
		v := r.view
		v.State = "requested"
		if r.cancel != nil {
			v.State = "authorizing"
		}
		out = append(out, v)
	}
	if len(out) > 0 {
		return out
	}
	if done, ok := s.last[agentID]; ok {
		return []Shared.MCPConnectRequestView{done}
	}
	if done, ok := s.settledFromStore(agentID); ok {
		return []Shared.MCPConnectRequestView{done}
	}
	return out
}
