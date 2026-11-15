package Scheduler

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"

	"github.com/anyscapelabs/argus/Services/Chat"
	"github.com/anyscapelabs/argus/Services/MCP"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
	"github.com/google/uuid"
)

// AgentRunner is the slice the engine needs from an unattended agent
// backend: whether an agent is mid-run, and the ability to start its run.
// The desktop build satisfies it with the Chat service directly; the HTTP
// deployment satisfies it with LoopRunner below, which drives the generic
// Chat.RunAgentLoop.
type AgentRunner interface {
	WakeAgent(agentID string)
	IsBusy(agentID string) bool
}

// LoopRunner is the scheduler's chat backend for the HTTP deployment: the
// same chatRunner contract SendScheduledWake and OnRunFinished spell out,
// implemented on top of the generic loop instead of the Wails event pump.
// A wake runs one loop to completion and reports the outcome through the
// registered callback, so the scheduler's bookkeeping is unchanged.
type LoopRunner struct {
	db   *sql.DB
	mgr  *MCP.Manager
	turn Chat.ModelTurn

	mu    sync.Mutex
	busy  map[string]bool
	done  func(Chat.RunOutcome)
	wakes chan string
}

// NewAgentRunner wires the generic loop into an AgentRunner. The turn
// callback is the model caller; tools and tier enforcement come from the
// shared guarded executor, so scheduled runs have exactly the surface the
// delegation loop promises.
func NewAgentRunner(db *sql.DB, mgr *MCP.Manager, turn Chat.ModelTurn) *LoopRunner {
	return &LoopRunner{
		db:    db,
		mgr:   mgr,
		turn:  turn,
		busy:  map[string]bool{},
		wakes: make(chan string, 64),
	}
}

// OnRunFinished registers the engine's outcome callback (chatRunner part).
func (r *LoopRunner) OnRunFinished(fn func(Chat.RunOutcome)) {
	r.mu.Lock()
	r.done = fn
	r.mu.Unlock()
}

// IsBusy reports whether the agent has a loop running right now.
func (r *LoopRunner) IsBusy(agentID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.busy[agentID]
}

// WakeAgent queues a start request for the agent. It never blocks: the pump
// goroutine owns every run, so a caller that wakes while busy just coalesces.
func (r *LoopRunner) WakeAgent(agentID string) {
	select {
	case r.wakes <- agentID:
	default:
	}
}

// SendScheduledWake starts the turn a scheduled task's fire produces
// (chatRunner part). Busy is a clean refusal — the scheduler keeps the task
// due and retries — and a started run is reported through prepared before
// any model work, matching the desktop runner's ordering.
func (r *LoopRunner) SendScheduledWake(agentID, text string, prepared func(Shared.SendResult)) Shared.SendResult {
	r.mu.Lock()
	if r.busy[agentID] {
		r.mu.Unlock()
		return Shared.SendResult{Ok: false, Error: Shared.NewError(Shared.CodeBusy, "Agent is already running")}
	}
	r.busy[agentID] = true
	r.mu.Unlock()

	runID := "run_" + uuid.NewString()
	if prepared != nil {
		prepared(Shared.SendResult{Ok: true, RunID: runID})
	}
	go r.run(runID, agentID, text, nil)
	return Shared.SendResult{Ok: true, RunID: runID}
}

// run executes one scheduled turn: the wake text as the opening user turn,
// guarded tools, and the outcome handed to the engine when the loop settles.
func (r *LoopRunner) run(runID, agentID, text string, mail *Storage.AgentMessage) {
	defer func() {
		r.mu.Lock()
		delete(r.busy, agentID)
		r.mu.Unlock()
		r.WakeAgent(agentID)
	}()

	threadID := "thr_" + uuid.NewString()
	hops := 0
	delegated := false
	if mail != nil {
		if mail.ThreadID != "" {
			threadID = mail.ThreadID
		}
		hops = mail.Hops
		delegated = mail.Kind == "request"
	}
	tools := Chat.TeamToolSpecs()
	if r.mgr != nil {
		if projected, err := Chat.BuildAgentMCPTools(context.Background(), r.db, r.mgr, agentID); err == nil {
			tools = append(tools, projected...)
		}
	}
	cfg := Chat.LoopConfig{
		AgentID:  agentID,
		ThreadID: threadID,
		System:   Chat.StoredSystemPrompt(r.db, agentID, true, delegated),
		Opening:  text,
		BaseHops: hops,
		Tools:    tools,
		Turn:     r.turn,
		Exec: func(ctx context.Context, cfg Chat.LoopConfig, call Chat.ToolCall) (string, error) {
			return Chat.ExecuteGuardedTool(ctx, r.db, r.mgr, cfg.AgentID, cfg.ThreadID, cfg.BaseHops, call.Name, call.Args, r.WakeAgent)
		},
	}
	response, err := Chat.RunAgentLoop(context.Background(), r.db, cfg)
	if mail != nil {
		status := "completed"
		report := response.Text
		if err != nil {
			status = "failed"
			report = "Delegated run failed: " + err.Error()
		}
		if mail.Kind == "request" {
			if finalizeErr := Chat.FinalizeDelegatedRun(r.db, *mail, report, status); finalizeErr != nil {
				log.Printf("scheduler: could not finalize delegated request %s: %v", mail.ID, finalizeErr)
			} else if mail.ReportBack {
				r.WakeAgent(mail.FromAgentID)
			}
		} else {
			_ = Storage.MarkAgentMessageStatus(r.db, mail.ID, status)
		}
	}

	out := Chat.RunOutcome{RunID: runID, AgentID: agentID, Status: "success"}
	if err != nil {
		out.Status = "error"
		log.Printf("scheduler: unattended run for %s failed: %v", agentID, err)
	}
	r.mu.Lock()
	done := r.done
	r.mu.Unlock()
	if done != nil {
		done(out)
	}
}

// Pump drains wake requests forever; the HTTP build starts it after wiring.
// A wake for a busy agent is dropped: the loop itself drains the inbox at
// every round boundary, so queued work is not lost — it is picked up by the
// run already in flight.
func (r *LoopRunner) Pump(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case agentID := <-r.wakes:
			if r.IsBusy(agentID) {
				continue
			}
			// A raw wake means queued mail exists for this agent; run with
			// an empty opening turn so the inbox drain speaks first.
			r.mu.Lock()
			busyNow := r.busy[agentID]
			if !busyNow {
				r.busy[agentID] = true
			}
			r.mu.Unlock()
			if busyNow {
				continue
			}
			mail, found, err := Storage.ClaimNextAgentMessage(r.db, agentID)
			if err != nil || !found {
				r.mu.Lock()
				delete(r.busy, agentID)
				r.mu.Unlock()
				continue
			}
			text := mail.Body
			switch mail.Kind {
			case "request":
				text = fmt.Sprintf("[Delegated task from %s] %s", mail.FromAgentID, mail.Body)
			case "report":
				text = fmt.Sprintf("[Report from %s] %s", mail.FromAgentID, mail.Body)
			}
			go r.run("run_"+uuid.NewString(), agentID, text, &mail)
		}
	}
}
