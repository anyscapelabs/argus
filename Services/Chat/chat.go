package Chat

import (
	"context"
	"errors"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/google/uuid"

	"github.com/anyscapelabs/argus/Keyring"
	"github.com/anyscapelabs/argus/Models"
	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Router"
	"github.com/anyscapelabs/argus/Services/Artifacts"
	"github.com/anyscapelabs/argus/Services/Browser"
	"github.com/anyscapelabs/argus/Services/MCP"
	"github.com/anyscapelabs/argus/Services/MCPConnect"
	"github.com/anyscapelabs/argus/Services/Permission"
	"github.com/anyscapelabs/argus/Services/Shared"
)

type Service struct {
	Core *Shared.Core
	// Permissions is the permission gate every tool call must pass.
	Permissions *Permission.Service
	// MCP is the MCP manager service.
	MCP *MCP.Service
	// MCPConnect handles MCP connection approval flow.
	MCPConnect *MCPConnect.Service
	// Artifacts stores the documents the agent writes. Unattended-safe by
	// design: unlike the file tools it needs no permission grant, which is what
	// lets a routine produce a report on a machine nobody is sitting at.
	Artifacts *Artifacts.Service
	// Browser drives Chrome for the agent. Reading a page is free; the
	// irreversible half of browsing goes through the permission gate, so an
	// unattended routine can look but not submit. Nil means this build has no
	// browser support, and the tool is not offered at all.
	Browser *Browser.Service

	mu       sync.Mutex
	cancels  map[string]context.CancelFunc
	agentRun map[string]string // agentID -> runID

	// liveSessions own the cancellable, serialized execution context behind the
	// Live API's compact search_tools/invoke_tool gateway.
	liveMu       sync.Mutex
	liveSessions map[string]*liveToolSession

	// takeovers is the browser human-takeover card state: one live card per
	// agent, plus when a captcha was last asked about. See takeover.go.
	takeoverStore Shared.InteractionStore[*userTakeover]
	askUserStore  Shared.InteractionStore[*pendingUserQuestion]
	captchaAskAt  map[string]time.Time
	captchaMu     sync.Mutex

	// startedAtMs is when the app booted; the right panel reads uptime off
	// it instead of the backend ticking a timer.
	startedAtMs int64

	doneMu       sync.Mutex
	doneHandlers []func(RunOutcome)
}

// RunOutcome is the summary of a finished agent run. The scheduler subscribes
// to these to close the loop on scheduled fires.
type RunOutcome struct {
	RunID              string
	AgentID            string
	AssistantMessageID string
	Status             string // success | error | cancelled
}

// OnRunFinished registers a callback fired when any run reaches a final
// state. Register before the first send; handlers run off the run goroutine.
func (s *Service) OnRunFinished(fn func(RunOutcome)) {
	s.doneMu.Lock()
	s.doneHandlers = append(s.doneHandlers, fn)
	s.doneMu.Unlock()
}

func (s *Service) runFinished(out RunOutcome) {
	s.doneMu.Lock()
	handlers := append([]func(RunOutcome){}, s.doneHandlers...)
	s.doneMu.Unlock()
	for _, fn := range handlers {
		fn(out)
	}
}

func (s *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	s.startedAtMs = time.Now().UnixMilli()
	if err := s.Core.Bootstrap(ctx); err != nil {
		return err
	}
	// The browser is the agent's, not the UI's, so it is started here rather than
	// registered as a bound service. It opens nothing until the first tool call.
	if s.Browser != nil {
		if cacheDir, err := Shared.CacheDir(); err == nil {
			_ = s.Browser.Start(filepath.Join(cacheDir, "browser-captures"))
		}
	}
	if err := s.Core.DB.SweepInterrupted(); err != nil {
		return err
	}
	// A turn that never got its reply stays exactly as it was written. This used
	// to be a DELETE of "unresponded" user messages, and because it identified
	// them by the row sitting at seq+1 — a seq the send path deliberately
	// abandons when it parks a reply behind its tool rounds — it deleted the
	// question of every tool-using turn, one startup at a time. Now the count is
	// only a log line, and the transcript keeps what the user typed.
	unanswered, err := s.Core.DB.UnansweredTurnsAll()
	if err != nil {
		return err
	}
	if unanswered > 0 {
		log.Printf("chat: %d turn(s) never received a reply; kept as written", unanswered)
	}
	return nil
}

// ServiceShutdown cancels every in-flight run. The signature matters: Wails
// asserts a ServiceShutdown interface whose method takes no context, so a
// ServiceShutdown(ctx) variant is silently never called.
func (s *Service) ServiceShutdown() error {
	s.StopAll()
	s.endAllLiveSessions()
	if s.Browser != nil {
		// Cancelling the runs above can leave a click mid-flight; by now it has
		// returned, and Chrome must go with the app rather than outlive it.
		return s.Browser.Shutdown()
	}
	return nil
}

func (s *Service) Stream(req Shared.StreamRequest) Shared.StreamStart {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.StreamStart{Error: aerr}
	}
	providerID, modelID := Models.SplitRef(strings.TrimSpace(req.Ref))
	if providerID == "" || modelID == "" {
		return Shared.StreamStart{Error: Shared.NewError(Shared.CodeBadInput, "Pick a provider and a model before streaming")}
	}
	provider, ok := s.Core.Store.Get().Provider(providerID)
	if !ok {
		return Shared.StreamStart{Error: Shared.NewError(Shared.CodeUnknownProvider, "Unknown provider")}
	}
	if cfg, found, _ := s.Core.DB.GetProviderConfig(providerID); found && strings.TrimSpace(cfg.BaseURLOverride) != "" {
		provider.BaseURL = strings.TrimSpace(cfg.BaseURLOverride)
	}
	overlayProvider, _ := s.Core.Overlay.Lookup(providerID)
	key := ""
	if !provider.NoAuth {
		stored, kerr := Keyring.Get(providerID)
		if kerr != nil {
			if errors.Is(kerr, Keyring.ErrNotFound) {
				return Shared.StreamStart{Error: Shared.NewError(Shared.CodeNotConfigured, "Connect "+providerID+" before streaming")}
			}
			return Shared.StreamStart{Error: Shared.NewError(Shared.CodeStorage, "The secrets store is unreachable. Re-enter the key")}
		}
		if strings.TrimSpace(stored) == "" {
			return Shared.StreamStart{Error: Shared.NewError(Shared.CodeNotConfigured, "Connect "+providerID+" before streaming")}
		}
		key = stored
	}
	target, terr := Router.TargetFor(provider, modelID, key, Router.TargetOptions{
		ExtraHeaders: overlayProvider.ExtraHeaders,
		Quirks:       overlayProvider.Quirks,
	})
	if terr != nil {
		return Shared.StreamStart{Error: Shared.NewError(Shared.CodeBadInput, terr.Error())}
	}
	cReq := Protocol.Request{
		Model:           modelID,
		System:          req.System,
		Messages:        req.Messages,
		MaxOutputTokens: req.MaxOutputTokens,
		Temperature:     req.Temperature,
		StopSequences:   req.StopSequences,
		Reasoning:       reasoningLevel(req.Reasoning),
	}
	runID, ctx, cancel := s.register()
	go s.run(runID, ctx, cancel, &cReq, &target)
	return Shared.StreamStart{Ok: true, RunID: runID}
}

func (s *Service) Stop(runID string) Shared.ActionResult {
	cancel, ok := s.take(runID)
	if !ok {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeNotFound, "No running model stream with that id")}
	}
	s.mu.Lock()
	delete(s.cancels, runID)
	for agentID, r := range s.agentRun {
		if r == runID {
			delete(s.agentRun, agentID)
		}
	}
	s.mu.Unlock()
	cancel()
	return Shared.ActionResult{Ok: true}
}

func (s *Service) StopAll() {
	s.mu.Lock()
	cancels := s.cancels
	s.cancels = map[string]context.CancelFunc{}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (s *Service) Active() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.cancels))
	for id := range s.cancels {
		ids = append(ids, id)
	}
	return ids
}

func (s *Service) register() (string, context.Context, context.CancelFunc) {
	id, _ := uuid.NewV7()
	runID := id.String()
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	if s.cancels == nil {
		s.cancels = map[string]context.CancelFunc{}
	}
	s.cancels[runID] = cancel
	return runID, ctx, cancel
}

func (s *Service) take(runID string) (context.CancelFunc, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cancel, ok := s.cancels[runID]
	return cancel, ok
}

func (s *Service) run(runID string, ctx context.Context, cancel context.CancelFunc, req *Protocol.Request, target *Protocol.Target) {
	terminal := false
	for ev := range Router.New().Run(ctx, req, target, nil) {
		if ev.Type == Protocol.EventMessageStop || ev.Type == Protocol.EventError {
			terminal = true
		}
		s.Core.EmitEvent(Shared.EventModelStream, Shared.StreamEvent{RunID: runID, Event: ev})
	}
	s.mu.Lock()
	delete(s.cancels, runID)
	s.mu.Unlock()
	cancel()
	if !terminal {
		s.Core.EmitEvent(Shared.EventStreamCancelled, Shared.StreamEvent{
			RunID: runID,
			Event: Protocol.Event{Type: Protocol.EventError, Error: Protocol.CancelledError()},
		})
	}
}

func reasoningLevel(raw string) Protocol.ReasoningLevel {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "none":
		return Protocol.ReasoningNone
	case "low":
		return Protocol.ReasoningLow
	case "medium":
		return Protocol.ReasoningMedium
	case "high":
		return Protocol.ReasoningHigh
	}
	return Protocol.ReasoningUnset
}

// Status is the small dashboard the right panel's Details tab renders: when
// the app started (uptime), when the agent last finished a turn (last
// active), and whether a run is on right now (online vs working). One
// snapshot per call; the panel recomputes the relative wording locally.
func (s *Service) Status(agentID string) Shared.AgentStatusResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.AgentStatusResult{Error: aerr}
	}
	last, err := s.Core.DB.LastActiveMs(agentID)
	if err != nil {
		return Shared.AgentStatusResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	s.mu.Lock()
	busy := len(s.cancels) > 0
	s.mu.Unlock()
	return Shared.AgentStatusResult{
		Status: Shared.AgentStatusView{
			StartedAtMs:  s.startedAtMs,
			LastActiveMs: last,
			Busy:         busy,
		},
	}
}
