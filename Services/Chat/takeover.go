package Chat

// The browser takeover flow, and why it lives in Chat: Chat is the one place
// that already owns the browser service, can emit the events the card uses,
// and can steer the agent with a turn nobody sees when the handover ends —
// all three without a new import edge.
//
// The shape is the MCPConnect card's: a request raised from the app side, one
// live card per agent, a Wails-bound Respond the UI's buttons call, and an
// in-memory state the frontend can re-read on remount. States: requested →
// active (the user is driving) → done; declined ends it too. The bookkeeping
// (one live card per agent) rides Shared.InteractionStore, shared with that
// card so both can't drift.

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/anyscapelabs/argus/Services/Shared"
)

// takeoverStates the frontend knows.
const (
	takeoverRequested = "requested"
	takeoverActive    = "active"
	takeoverDone      = "done"
	takeoverDeclined  = "declined"
)

// takeoverCooldown is how long one captcha detection buys before the card may
// be raised again on the same page: a challenge the user is solving must not
// re-offer itself while it sits half-typed.
const takeoverCooldown = 2 * time.Minute

// captchaPatterns is the concrete widget names plus the strongest human-check
// wordings. The bare word "challenge" is deliberately absent — a page about
// chess puzzles is not a bot wall, and a false card is worse than no card.
var captchaPatterns = regexp.MustCompile(
	`(?i)(g-recaptcha|recaptcha|google-challenge|h-?captcha|cf-?turnstile|turnstile|challenge-platform|verify you are(?: a)? human|are you a robot|not a robot|prove you(?:'|’)re(?: a)? human)`,
)

// userTakeover holds one card from raise to settlement.
type userTakeover struct {
	view Shared.TakeoverRequestView
}

// captchaDetected answers whether snapshot text carries a challenge the agent
// cannot pass alone.
func captchaDetected(text string) bool {
	return captchaPatterns.MatchString(text)
}

// captchaArrived applies the per-agent cooldown: one page yields one card,
// not one per browser call while the user works.
func (s *Service) captchaArrived(agentID string) bool {
	s.captchaMu.Lock()
	defer s.captchaMu.Unlock()
	if s.captchaAskAt == nil {
		s.captchaAskAt = map[string]time.Time{}
	}
	if last, ok := s.captchaAskAt[agentID]; ok && time.Since(last) < takeoverCooldown {
		return false
	}
	s.captchaAskAt[agentID] = time.Now()
	return true
}

// RegisterTakeover raises the card: screenshot the page as it stands, park the
// request, and tell the UI. One live card per agent — a second register while
// the first is unanswered or in flight returns the existing card instead of
// stacking a second ask on top of the first.
func (s *Service) RegisterTakeover(agentID, agentName, reason string) (Shared.TakeoverRequestView, error) {
	if s.Browser == nil {
		return Shared.TakeoverRequestView{}, fmt.Errorf("browser: this build has no browser support")
	}
	if r, ok := s.takeoverStore.Live(agentID); ok {
		return r.view, nil
	}

	if s.Browser.TakenOver() {
		return Shared.TakeoverRequestView{}, fmt.Errorf("browser: the user is already driving the browser")
	}
	var screenshot, host, title string
	// A failed capture still yields a card — "I can't show you the page, but
	// please take over" beats silently continuing past a captcha.
	if data, h, t, err := s.Browser.CaptureForUI(context.Background()); err == nil {
		screenshot, host, title = data, h, t
	}

	id, _ := uuid.NewV7()
	r := &userTakeover{view: Shared.TakeoverRequestView{
		RequestID:  id.String(),
		AgentID:    agentID,
		AgentName:  agentName,
		Reason:     reason,
		Screenshot: screenshot,
		Host:       host,
		PageTitle:  title,
		State:      takeoverRequested,
		CreatedAt:  time.Now().UnixMilli(),
	}}
	if !s.takeoverStore.Begin(agentID, r, r.view.RequestID) {
		// The slot filled between the check and the park; give back the
		// card that won.
		if live, ok := s.takeoverStore.Live(agentID); ok {
			return live.view, nil
		}
		return r.view, fmt.Errorf("browser: the user is already driving the browser")
	}
	s.Browser.BlockForTakeover()
	s.Core.EmitEvent(Shared.EventTakeoverRequested, r.view)
	return r.view, nil
}

// RespondTakeover is the binding the card's buttons call. decisions:
// takeover | done | decline.
func (s *Service) RespondTakeover(requestID, decision string) Shared.ActionResult {
	r, ok := s.takeoverStore.Get(requestID)
	if !ok {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeNotFound, "That takeover request is no longer active")}
	}
	switch decision {
	case "takeover":
		if r.view.State != takeoverRequested {
			return Shared.ActionResult{Error: Shared.NewError(Shared.CodeBadInput, "That browser handover is not waiting to be taken over")}
		}
		// Hand the window over first; the card only says it is driving once
		// Chrome has actually come to the front.
		if err := s.Browser.TakeOver(context.Background()); err != nil {
			s.Browser.HandBack()
			s.finishTakeover(r, takeoverDeclined, err.Error())
			return Shared.ActionResult{Ok: true}
		}
		s.emitTakeover(r, takeoverActive, "")
	case "done":
		if r.view.State != takeoverActive {
			return Shared.ActionResult{Error: Shared.NewError(Shared.CodeBadInput, "The browser is not currently handed over")}
		}
		s.Browser.HandBack()
		s.emitTakeover(r, takeoverDone, "")
		s.steerTakeover(r, takeoverDone)
	case "decline":
		if r.view.State != takeoverRequested {
			return Shared.ActionResult{Error: Shared.NewError(Shared.CodeBadInput, "That handover can no longer be declined")}
		}
		s.Browser.CancelTakeover()
		s.emitTakeover(r, takeoverDeclined, "")
		s.steerTakeover(r, takeoverDeclined)
	default:
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeBadInput, "Unknown decision")}
	}
	if decision != "takeover" {
		s.takeoverStore.End(requestID, r.view.AgentID)
	}
	return Shared.ActionResult{Ok: true}
}

// PendingTakeover restores the card after a remount. A live card comes back
// with its current state; a settled one does not survive as UI — the notice
// row beside it carries what happened.
func (s *Service) PendingTakeover(agentID string) Shared.TakeoverListResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.TakeoverListResult{Error: aerr}
	}
	out := []Shared.TakeoverRequestView{}
	if r, ok := s.takeoverStore.Live(agentID); ok {
		v := r.view
		if v.State == takeoverRequested && s.Browser != nil && s.Browser.TakenOver() {
			v.State = takeoverActive
		}
		out = append(out, v)
	}
	return Shared.TakeoverListResult{Takeovers: out}
}

func (s *Service) emitTakeover(r *userTakeover, state, msg string) {
	r.view.State = state
	s.Core.EmitEvent(Shared.EventTakeoverStatus, map[string]any{
		"request_id": r.view.RequestID,
		"agent_id":   r.view.AgentID,
		"state":      state,
		"error":      msg,
		"host":       r.view.Host,
		"created_at": r.view.CreatedAt,
	})
}

// finishTakeover is for endings that come from the backend side (a failed
// TakeOver call): the notice row keeps what happened.
func (s *Service) finishTakeover(r *userTakeover, state, msg string) {
	if s.Browser != nil {
		s.Browser.HandBack()
	}
	s.emitTakeover(r, state, msg)
	s.persistTakeoverNotice(r, state, msg)
	s.takeoverStore.End(r.view.RequestID, r.view.AgentID)
	// The run that raised the card was told to stop and wait for a handover that
	// never happened, so it needs the same invisible turn that "done" and
	// "decline" send — otherwise the agent sits silent while the user reads a
	// notice about a browser nobody ever drove.
	if state == takeoverDeclined {
		why := msg
		if why == "" {
			why = "the browser could not be handed over"
		}
		text := fmt.Sprintf("(The browser handover you asked for did not happen: %s. Nobody is driving the page. Continue without it — do what you can yourself, and say plainly in chat which step you still need the user for.)", why)
		if res := s.Steer(r.view.AgentID, text); res.Error != nil {
			log.Printf("takeover: steering run for %s refused: %s", r.view.AgentID, res.Error.Message)
		}
	}
}

// steerTakeover hands the turn back to the agent the same way the MCP connect
// settle does: an invisible steering turn that says what the user did and
// where the page stands. The run that raised the card has long finished — its
// tool result told the model to wait — so the steer starts the fresh run the
// work resumes in.
func (s *Service) steerTakeover(r *userTakeover, state string) {
	s.persistTakeoverNotice(r, state, "")
	text := takeoverSteerText(r, state)
	if text == "" {
		return
	}
	if res := s.Steer(r.view.AgentID, text); res.Error != nil {
		log.Printf("takeover: steering run for %s refused: %s", r.view.AgentID, res.Error.Message)
	}
}

func takeoverSteerText(r *userTakeover, state string) string {
	where := "it"
	if r.view.Host != "" {
		where = r.view.Host
	}
	switch state {
	case takeoverDone:
		return fmt.Sprintf("(The user finished by hand whatever the page on %s needed the browser for — personal details, a sign-in, or a CAPTCHA — and handed control back to you. Continue the task from a fresh read of where the page stands now; do not redo their steps.)", where)
	case takeoverDeclined:
		return "(The user declined to take over the browser. Continue with what you can do without their hands, or say plainly which step you need them for.)"
	}
	return ""
}

// noticeTextForTakeover is the sentence the conversation keeps after the card
// ends. Kind "takeover" keeps the row out of the permission cards' wardrobe.
func noticeTextForTakeover(v Shared.TakeoverRequestView, state, msg string) string {
	switch state {
	case takeoverDone:
		return "Browser handover finished: the user acted on " + whereOf(v) + " and gave control back"
	case takeoverDeclined:
		if msg != "" {
			return "Browser handover ended: " + msg
		}
		return "Browser handover declined"
	}
	return "Browser handover " + state
}

func whereOf(v Shared.TakeoverRequestView) string {
	if v.Host != "" {
		return v.Host
	}
	return "the page"
}

// persistTakeoverNotice writes the settled state into the notices table.
func (s *Service) persistTakeoverNotice(r *userTakeover, state, msg string) {
	Shared.PersistNotice(s.Core, r.view.AgentID, r.view.AgentName, "browser", state, "takeover", noticeTextForTakeover(r.view, state, msg))
}
