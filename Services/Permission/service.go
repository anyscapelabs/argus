package Permission

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
	"github.com/google/uuid"
)

// The tool families the permission gate guards. A grant is stored per tool, so
// "always allow terminal" says nothing about files and vice versa.
const (
	TerminalTool   = "execute_command"
	FileAccessTool = "file_access"
	// BrowserTool guards the irreversible half of browsing: buying, sending,
	// posting, deleting. Reading is free and never reaches the gate.
	BrowserTool = "browser_action"
	// BrowserUseTool controls whether an agent may drive the browser at all.
	// It is separate from BrowserTool, which only gates irreversible actions.
	BrowserUseTool = "browser_use"
	// SkillInstallTool guards enabling/installing an Agent Skill.
	SkillInstallTool = "skill_install"
	// EmailSendTool guards sending email on the user's behalf: a draft is
	// reversible, a sent message is not.
	EmailSendTool = "email_send"
)

const (
	GlobalAgentID = "*"
	PolicyAsk     = "ask"
	PolicyInherit = "inherit"
)

var policyTools = []string{TerminalTool, FileAccessTool, BrowserTool, SkillInstallTool, BrowserUseTool, EmailSendTool}

// BrowserHostTool scopes a browsing grant to one site. "Always allow" then means
// "on this site", which is what a user means when they click it on a card, and it
// is the reason Revoke has to sweep the family by prefix.
func BrowserHostTool(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return BrowserTool
	}
	return BrowserTool + ":" + host
}

// IsBrowserGrant reports whether a stored tool string belongs to the browsing
// family, host-scoped or not.
func IsBrowserGrant(tool string) bool {
	return tool == BrowserTool || strings.HasPrefix(tool, BrowserTool+":")
}

// Decision values the gate and the frontend agree on.
const (
	AllowOnce   = "allow_once"
	AllowAlways = "allow_always"
	Deny        = "deny"
	// Deferred is returned instead of Deny when an unattended run asks for a
	// permission nobody is present to grant. The action was held, not refused
	// by the user: callers tell the model to report it and expect to be asked
	// again on the next interactive turn.
	Deferred = "deferred"
)

// DeferralNoticeText is both the notice the user sees woven into the chat and
// the tool result the model reads. It names the held action and says plainly
// that replying is what lets the agent ask permission and finish the work —
// an unattended gate must never look like a user refusal or fail silently.
func DeferralNoticeText(tool, command string) string {
	family := tool
	switch {
	case tool == TerminalTool:
		family = "a terminal command"
	case tool == FileAccessTool:
		family = "file access"
	case tool == EmailSendTool:
		family = "an email send"
	case tool == SkillInstallTool:
		family = "installing an agent skill"
	case IsBrowserGrant(tool):
		family = "a browser action"
		if host := strings.TrimPrefix(tool, BrowserTool+":"); host != tool {
			family += " on " + host
		}
	}
	if detail := strings.TrimSpace(command); detail != "" {
		family += ": " + detail
	}
	return fmt.Sprintf("I held %s because this background run has nobody to ask for permission, so nothing was changed. Reply to me and I'll ask permission, then try again. (Do not retry within this run — nobody can approve here.)", family)
}

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
			Kind:      n.Kind,
			Text:      n.Body,
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
	grant, found, err := resolveStoredGrant(s.Core.DB, agentID, TerminalTool)
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

// Policies returns the global defaults and per-agent overrides used by the
// settings UI. Browser use defaults on; sensitive actions default to asking.
func (s *Service) Policies(agentID string) Shared.PermissionPoliciesResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.PermissionPoliciesResult{Error: aerr}
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return Shared.PermissionPoliciesResult{Error: Shared.NewError(Shared.CodeBadInput, "Choose an agent")}
	}

	policies := make([]Shared.PermissionPolicyView, 0, len(policyTools))
	for _, tool := range policyTools {
		globalMode := defaultPolicy(tool)
		if grant, found, err := s.Core.DB.GetGrant(GlobalAgentID, tool); err != nil {
			return Shared.PermissionPoliciesResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
		} else if found {
			globalMode = grant.Mode
		}

		agentMode := PolicyInherit
		if agentID != GlobalAgentID {
			if grant, found, err := s.Core.DB.GetGrant(agentID, tool); err != nil {
				return Shared.PermissionPoliciesResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
			} else if found {
				agentMode = grant.Mode
			}
		}
		effective := globalMode
		if agentMode != PolicyInherit {
			effective = agentMode
		}
		policies = append(policies, Shared.PermissionPolicyView{
			Tool:          tool,
			GlobalMode:    globalMode,
			AgentMode:     agentMode,
			EffectiveMode: effective,
		})
	}
	return Shared.PermissionPoliciesResult{Policies: policies}
}

// SetPolicy writes a global default (agentID="*") or one agent override.
// Clearing the browser action family also removes old site-specific grants so
// the broad setting shown in the UI is the whole truth.
func (s *Service) SetPolicy(agentID, tool, mode string) Shared.ActionResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.ActionResult{Error: aerr}
	}
	agentID = strings.TrimSpace(agentID)
	tool = strings.TrimSpace(tool)
	mode = strings.TrimSpace(mode)
	if agentID == "" || !isPolicyTool(tool) {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeBadInput, "Unknown permission setting")}
	}

	clear := mode == PolicyAsk || mode == PolicyInherit
	if !clear && mode != string(Storage.GrantAllow) && mode != string(Storage.GrantDeny) {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeBadInput, "Unknown permission mode")}
	}
	if tool == BrowserTool {
		if err := s.Core.DB.ClearGrantPrefix(agentID, BrowserTool); err != nil {
			return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
		}
	} else if clear {
		if err := s.Core.DB.ClearGrant(agentID, tool); err != nil {
			return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
		}
	}
	if clear {
		return Shared.ActionResult{Ok: true}
	}
	if err := s.Core.DB.SetGrant(agentID, tool, Storage.GrantMode(mode)); err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	return Shared.ActionResult{Ok: true}
}

// IsBrowserAllowed is the runtime browser kill switch. With no saved policy,
// browsing remains available to preserve the existing behavior.
func (s *Service) IsBrowserAllowed(agentID string) bool {
	if s == nil || s.Core == nil || s.Core.DB == nil {
		return false
	}
	grant, found, err := resolveStoredGrant(s.Core.DB, agentID, BrowserUseTool)
	if err != nil {
		return false
	}
	return !found || grant.Mode != string(Storage.GrantDeny)
}

// Revoke removes the persisted grants of an agent — terminal, file access and
// every site-scoped browsing grant — so one revoke leaves nothing quietly still
// permitted.
func (s *Service) Revoke(agentID string) Shared.ActionResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.ActionResult{Error: aerr}
	}
	for _, tool := range []string{TerminalTool, FileAccessTool, SkillInstallTool, BrowserUseTool} {
		if err := s.Core.DB.ClearGrant(agentID, tool); err != nil {
			return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
		}
	}
	// Browsing grants are per site, so the family is swept by prefix rather than
	// listed; leaving one behind would keep the agent inside a user's inbox.
	if err := s.Core.DB.ClearGrantPrefix(agentID, BrowserTool); err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	return Shared.ActionResult{Ok: true}
}

// Request is the gate entry point used by the agent loop. It returns the
// stored decision when a grant exists, otherwise it emits a
// permission:requested event and blocks until the user answers through
// Respond or the context is cancelled.
func Request(ctx context.Context, svc *Service, agentID, agentName, tool, command string) (string, error) {
	grant, found, err := resolveStoredGrant(svc.Core.DB, agentID, tool)
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

	// Nobody is watching an unattended run, so an interactive ask would hang
	// the agent until the timeout. Hold the action and leave a notice the user
	// can read in the chat afterwards: it names what was held and says that
	// replying is what lets the agent ask permission and finish the work. The
	// routine must not fail silently, and it must not look like a user refusal.
	if Shared.IsUnattended(ctx) {
		if nid, uerr := uuid.NewV7(); uerr == nil {
			if serr := svc.Core.DB.InsertPermissionNotice(Storage.PermissionNotice{
				ID:        nid.String(),
				AgentID:   agentID,
				AgentName: agentName,
				Tool:      tool,
				Command:   command,
				Decision:  Deferred,
				Kind:      "permission",
				Body:      DeferralNoticeText(tool, command),
				CreatedAt: time.Now().UnixMilli(),
			}); serr != nil {
				log.Printf("permission: could not persist unattended deferral: %v", serr)
			}
		}
		return Deferred, nil
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

func defaultPolicy(tool string) string {
	if tool == BrowserUseTool {
		return string(Storage.GrantAllow)
	}
	return PolicyAsk
}

func isPolicyTool(tool string) bool {
	for _, allowed := range policyTools {
		if tool == allowed {
			return true
		}
	}
	return false
}

// resolveStoredGrant applies the narrowest override first: agent + host,
// agent family, global + host, then global family.
func resolveStoredGrant(db *Storage.DB, agentID, tool string) (Storage.Grant, bool, error) {
	agentID = strings.TrimSpace(agentID)
	tool = strings.TrimSpace(tool)
	type candidate struct{ agentID, tool string }
	candidates := []candidate{{agentID, tool}}
	if IsBrowserGrant(tool) && tool != BrowserTool {
		candidates = append(candidates, candidate{agentID, BrowserTool})
	}
	if agentID != GlobalAgentID {
		candidates = append(candidates, candidate{GlobalAgentID, tool})
		if IsBrowserGrant(tool) && tool != BrowserTool {
			candidates = append(candidates, candidate{GlobalAgentID, BrowserTool})
		}
	}
	for _, candidate := range candidates {
		grant, found, err := db.GetGrant(candidate.agentID, candidate.tool)
		if err != nil || found {
			return grant, found, err
		}
	}
	return Storage.Grant{}, false, nil
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
