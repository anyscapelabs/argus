package Chat

import (
	"log"
	"time"

	"github.com/anyscapelabs/argus/Protocol"
	Memory "github.com/anyscapelabs/argus/Services/Memory"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

func (s *Service) onboardingName(agentID string) string {
	profile, found, err := s.Core.DB.GetAgentProfile(agentID)
	if err == nil && found && profile.AgentName != "" {
		return profile.AgentName
	}
	return "Argus"
}

func (s *Service) setOnboardingStep(step string) error {
	return s.Core.DB.SetAppSetting(onboardingStepKey, step)
}

func (s *Service) appendOnboardingText(agentID, text string) {
	seq, err := s.Core.DB.NextSeq(agentID)
	if err != nil {
		return
	}
	now := nowUnixMs()
	blocks := mustMarshal([]Protocol.Block{{Type: Protocol.BlockText, Text: text}})
	if err := s.Core.DB.InsertMessage(Storage.Message{
		ID: newID(), AgentID: agentID, Seq: seq, Role: "assistant",
		BlocksJSON: blocks, Text: text, Status: "complete",
		CreatedAt: now, CompletedAt: now,
	}); err != nil {
		log.Printf("chat: could not persist onboarding message: %v", err)
		return
	}
	s.Core.EmitEvent(Shared.EventChatMessage, map[string]any{"agent_id": agentID})
}

func (s *Service) openOnboardingCard(agentID, question string, choices []string) *pendingUserQuestion {
	req, live := s.askUserStore.Live(agentID)
	if live {
		return req
	}
	req = &pendingUserQuestion{
		view: Shared.AskUserRequestView{
			RequestID: newID(), AgentID: agentID, AgentName: s.onboardingName(agentID),
			Question: question, Choices: choices, CreatedAt: nowUnixMs(),
		},
		done: make(chan struct{}),
	}
	if !s.askUserStore.Begin(agentID, req, req.view.RequestID) {
		return nil
	}
	s.Core.EmitEvent(Shared.EventAskUserRequested, req.view)
	return req
}

func (s *Service) askOnboardingQuestion(agentID, question string, choices []string) (string, bool) {
	req := s.openOnboardingCard(agentID, question, choices)
	if req == nil {
		return "", false
	}

	timer := time.NewTimer(10 * time.Minute)
	defer timer.Stop()
	select {
	case <-req.done:
		answer, state := req.outcome()
		return answer, state == "answered" && answer != ""
	case <-timer.C:
		req.settle("", "cancelled")
		s.finishAskUser(req, "cancelled")
		return "", false
	}
}

func (s *Service) runOnboardingSequence(agentID string) {
	s.onboardingMu.Lock()
	if s.onboardingRunning == nil {
		s.onboardingRunning = make(map[string]bool)
	}
	if s.onboardingRunning[agentID] {
		s.onboardingMu.Unlock()
		return
	}
	s.onboardingRunning[agentID] = true
	s.onboardingMu.Unlock()
	defer func() {
		s.onboardingMu.Lock()
		s.onboardingRunning[agentID] = false
		s.onboardingMu.Unlock()
	}()

	for {
		step, seen, err := s.Core.DB.GetAppSetting(onboardingStepKey)
		if err != nil || !seen {
			return
		}
		switch step {
		case onboardingStepName:
			answer, ok := s.askOnboardingQuestion(agentID,
				"What would you like to call me?",
				[]string{"Argus", "Atlas", "Sage", "Juno"})
			if !ok {
				return
			}
			if _, err := saveAgentName(s.Core.DB, agentID, answer); err != nil {
				log.Printf("chat: onboarding rename failed: %v", err)
				return
			}
			s.Core.EmitEvent(Shared.EventSlotsChanged, nil)
			if err := s.setOnboardingStep(onboardingStepRole); err != nil {
				return
			}

		case onboardingStepRole:
			answer, ok := s.askOnboardingQuestion(agentID,
				"What should my responsibility and role be?",
				[]string{"Chief of Staff", "Project Manager", "Research Assistant", "Personal Assistant"})
			if !ok {
				return
			}
			role := answer
			if len([]rune(role)) > 160 {
				role = string([]rune(role)[:160])
			}
			if _, err := HandleSetAgentRole(s.Core.DB.SQL(), agentID, role, ""); err != nil {
				log.Printf("chat: onboarding role save failed: %v", err)
				return
			}
			s.Core.EmitEvent(Shared.EventSlotsChanged, nil)
			if err := s.setOnboardingStep(onboardingStepUser); err != nil {
				return
			}

		case onboardingStepUser:
			answer, ok := s.askOnboardingQuestion(agentID,
				"What would you like me to call you?",
				[]string{"My first name", "My full name", "Anything is fine", "Skip"})
			if !ok {
				return
			}
			if err := s.Core.DB.SetAppSetting("user_preferred_name", answer); err != nil {
				return
			}
			if _, err := Memory.SaveStructured(s.Core.DB, Storage.Memory{
				AgentID: Storage.GlobalMemoryAgentID, Key: "preferred_name", Value: answer,
				Scope: "preference", Confidence: 1, SourceType: "verified_user_message",
			}); err != nil {
				log.Printf("chat: onboarding preferred name memory failed: %v", err)
			}
			if err := s.setOnboardingStep(onboardingStepConnect); err != nil {
				return
			}

		case onboardingStepConnect:
			s.appendOnboardingText(agentID, "You're all set. Let's connect Gmail so I can help with your email.")
			if s.MCPConnect != nil {
				s.MCPConnect.Register(agentID, s.onboardingName(agentID), "gmail", "", "", "", "draft")
			}
			if err := s.setOnboardingStep(onboardingStepDone); err != nil {
				return
			}
			_ = s.Core.DB.SetAppSetting(firstRunSettingKey, firstRunStateDone)
			return

		default:
			return
		}
	}
}
