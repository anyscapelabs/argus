package Chat

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/google/uuid"

	"github.com/anyscapelabs/argus/Keyring"
	"github.com/anyscapelabs/argus/Models"
	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Router"
	"github.com/anyscapelabs/argus/Services/Shared"
)

type Service struct {
	Core *Shared.Core

	mu       sync.Mutex
	cancels  map[string]context.CancelFunc
	agentRun map[string]string // agentID -> runID
}

func (s *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	if err := s.Core.Bootstrap(ctx); err != nil {
		return err
	}
	return s.Core.DB.SweepInterrupted()
}

func (s *Service) ServiceShutdown(ctx context.Context) error {
	s.StopAll()
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
