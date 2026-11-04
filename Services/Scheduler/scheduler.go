// Package Scheduler runs the background engine that fires routines and
// reminders. The database's next_run_at column is the source of truth; the
// timer is only a convenience sleep, so a closed app or a lost network never
// loses a schedule — missed work re-surfaces on the next sweep.
//
// Routines coalesce: one missed fire is caught up if it is still inside its
// own period window, otherwise the moment is simply gone and the schedule
// advances. Reminders always catch up — a birthday greeting three hours late
// beats no greeting.
package Scheduler

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	servicechat "github.com/anyscapelabs/argus/Services/Chat"
	"github.com/anyscapelabs/argus/Services/Scheduler/spec"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
	"github.com/google/uuid"
)

// chatRunner is the slice of the Chat service the engine needs. Narrow on
// purpose: the scheduler depends on a behaviour, not on 700 lines of send.go.
type chatRunner interface {
	SendScheduledWake(agentID, text string) Shared.SendResult
	OnRunFinished(fn func(servicechat.RunOutcome))
}

// Notifier is the notification sink; main.go passes Wails'
// notifications.NotificationService, but the engine only ever calls Notify.
type Notifier interface {
	Notify(title, body string)
}

type pendingRun struct {
	taskID string
	runID  string
	title  string
}

type Service struct {
	Core     *Shared.Core
	Chat     chatRunner
	Notifier Notifier

	mu         sync.Mutex
	pending    map[string]pendingRun    // chat run id -> the schedule that fired it
	failures   map[string]*failureState // task id -> send failures, for backoff
	lastBusyAt time.Time                // when an agent-busy retry was last seen
	ctx        context.Context
	cancel     context.CancelFunc
	onlineMu   sync.Mutex
	onlineAt   time.Time
	onlineOK   bool
}

// failureState counts consecutive send failures for one task. A reminder
// that could not start stays due — the user still owes that work — so the
// counter is what keeps a broken provider from being retried every sweep.
type failureState struct {
	count int
	until time.Time
}

const (
	maxSleep     = 60 * time.Second // the DB is truth; a long timer is never trusted
	offlineRetry = 30 * time.Second // re-probe interval while there is no network
	probeTimeout = 2 * time.Second
	onlineCache  = 20 * time.Second
)

// failureBackoff widens with consecutive failures: 30s, 2m, 10m, then every
// half hour. Retrying is right, spinning is not.
var failureBackoff = []time.Duration{
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
	30 * time.Minute,
}

func (s *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	if err := s.Core.Bootstrap(ctx); err != nil {
		return err
	}
	if err := s.Core.DB.SweepAbandonedRuns(); err != nil {
		log.Printf("scheduler: abandoned-run sweep failed: %v", err)
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	// The engine hears about finished runs through the chat service itself,
	// so it never has to poll for the assistant row to settle.
	s.Chat.OnRunFinished(s.runFinished)
	go s.loop()
	return nil
}

// ServiceShutdown stops the engine's loop. Note the signature: Wails'
// ServiceShutdown interface takes no context, and a variant that does is
// silently never called.
func (s *Service) ServiceShutdown() error {
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

func (s *Service) loop() {
	for {
		// The first sweep runs at boot: everything that came due while the app
		// was closed fires as soon as the machine is back and online.
		wait, err := s.sweep(s.ctx)
		if err != nil {
			log.Printf("scheduler: sweep failed: %v", err)
			// A broken sweep must not spin the loop against a dead database.
			wait = maxSleep
		}
		if wait > maxSleep {
			wait = maxSleep
		}
		if wait < time.Second {
			wait = time.Second
		}
		timer := time.NewTimer(wait)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			timer.Stop()
		}
	}
}

// scheduleDelay is how long the engine may sleep before the next work: zero
// when something is due and ready to fire, otherwise the distance to the next
// fire or to the earliest backoff release — capped by maxSleep either way.
func (s *Service) scheduleDelay() (time.Duration, error) {
	now := time.Now()
	nowMs := now.UnixMilli()
	due, err := s.Core.DB.DueScheduledTasks(nowMs)
	if err != nil {
		return 0, err
	}
	if len(due) > 0 {
		if ready := s.readyWait(due, now); ready > 0 {
			return ready, nil
		}
		s.mu.Lock()
		busyWait := time.Since(s.lastBusyAt) < 15*time.Second
		s.mu.Unlock()
		if busyWait {
			return 15 * time.Second, nil
		}
		return 0, nil
	}
	next, err := s.Core.DB.NextDueMs(nowMs)
	if err != nil {
		return 0, err
	}
	if next == 0 {
		return maxSleep, nil
	}
	d := time.Duration(next-nowMs) * time.Millisecond
	if d < 0 {
		d = 0
	}
	return d, nil
}

// readyWait is the shortest remaining backoff across the due tasks, zero as
// soon as any one of them is free to attempt.
func (s *Service) readyWait(due []Storage.ScheduledTask, now time.Time) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	wait := time.Duration(-1)
	for _, t := range due {
		remaining := time.Duration(0)
		if f := s.failures[t.ID]; f != nil {
			remaining = time.Until(f.until)
			if remaining < 0 {
				remaining = 0
			}
		}
		if remaining == 0 {
			return 0
		}
		if wait < 0 || remaining < wait {
			wait = remaining
		}
	}
	if wait < 0 {
		return 0
	}
	return wait
}

// suppressed reports whether a task is sitting out a failure backoff.
func (s *Service) suppressed(id string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.failures[id]
	if f == nil {
		return false
	}
	if now.Before(f.until) {
		return true
	}
	delete(s.failures, id)
	return false
}

// noteFailure widens a task's backoff and returns how long it now waits.
func (s *Service) noteFailure(id string, now time.Time) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failures == nil {
		s.failures = map[string]*failureState{}
	}
	f := s.failures[id]
	if f == nil {
		f = &failureState{}
		s.failures[id] = f
	}
	f.count++
	step := failureBackoff[len(failureBackoff)-1]
	if f.count-1 < len(failureBackoff) {
		step = failureBackoff[f.count-1]
	}
	f.until = now.Add(step)
	// A cancelled schedule's state would otherwise live forever; expired rows
	// cost nothing to drop once the map stops being trivially small.
	if len(s.failures) > 32 {
		for k, v := range s.failures {
			if now.After(v.until) {
				delete(s.failures, k)
			}
		}
	}
	return step
}

func (s *Service) clearFailure(id string) {
	s.mu.Lock()
	delete(s.failures, id)
	s.mu.Unlock()
}

// sweep attempts every due task and reports how long the caller should wait
// before the next one.
func (s *Service) sweep(ctx context.Context) (time.Duration, error) {
	// No network means no model calls; leave every task due and retry on the
	// probe interval instead of burning schedules against an offline agent.
	if !s.online() {
		return offlineRetry, nil
	}
	now := time.Now()
	due, err := s.Core.DB.DueScheduledTasks(now.UnixMilli())
	if err != nil {
		return 0, err
	}
	for _, t := range due {
		if ctx.Err() != nil {
			return 0, nil
		}
		if s.suppressed(t.ID, now) {
			continue
		}
		if err := s.fire(t, now.UnixMilli()); err != nil {
			log.Printf("scheduler: task %s (%s): %v", t.ID, t.Title, err)
		}
	}
	return s.scheduleDelay()
}

// fire executes one due task exactly once. A routine's next fire is written
// to the database before the agent is asked to work, so a crash or a failed
// run can never make it run twice; only a busy agent lets it retry, since
// nothing was started in that case.
func (s *Service) fire(t Storage.ScheduledTask, now int64) error {
	// Whatever this attempt does to the row — advance it, retire it, or stamp
	// a failure on it — the panel is told once, on the way out.
	defer s.notifyChanged()
	sp, err := spec.Decode(t.ScheduleJSON)
	if err != nil {
		_ = s.Core.DB.MarkTaskFired(t.ID, now, "spec unreadable: "+err.Error(), t.Kind == Storage.KindReminder)
		return fmt.Errorf("unreadable schedule: %w", err)
	}

	agentID := t.AgentID
	if agentID == "" {
		agentID = "default"
	}

	late := now - t.NextRunAt
	kindLabel := "routine"
	if t.Kind == Storage.KindReminder {
		kindLabel = "reminder"
	}
	// The wake text is what the model turns into behaviour, so it says exactly
	// what a fire is: an app-authored job the agent already agreed to — not a
	// user message, and emphatically not a scheduling request. The old shape
	// ('[scheduled routine · title] <prompt>') read like a message that merely
	// mentioned the instruction, and the agent's answer was to set the routine
	// up again instead of doing it.
	var wake strings.Builder
	fmt.Fprintf(&wake,
		"Your scheduler woke you: the %s %q you set up earlier is due now. There is no user message — this is an internal app-authored turn.\n"+
			"Stored instruction: %s\n"+
			"Execute that instruction now, then report the result to the user in this conversation. "+
			"Do not call create_routine or schedule_reminder as part of this turn — the task is already scheduled; your job is only to perform it. "+
			"Do not ask the user questions; this runs unattended.", kindLabel, t.Title, strings.TrimSpace(t.Prompt))
	if late > 0 {
		wake.WriteString("\n(Time note: this fired " + humanDelay(late) + lateLate(late) + " — factor that into how you execute it.)")
	}

	if t.Kind == Storage.KindRoutine {
		if late > sp.Window().Milliseconds() {
			// Older than the period that produced it — coalesce into the
			// next fire instead of running stale work.
			next, nerr := s.nextFire(sp, now, t)
			if nerr != nil {
				return nerr
			}
			if err := s.Core.DB.SetTaskSchedule(t.ID, t.ScheduleJSON, next); err != nil {
				return err
			}
			log.Printf("scheduler: routine %s missed %s ago, beyond its window — advanced", t.Title, humanDelay(late))
			return nil
		}
		next, nerr := s.nextFire(sp, now, t)
		if nerr != nil {
			return nerr
		}
		// Advance BEFORE firing: no path through a crash re-runs this fire.
		if err := s.Core.DB.SetTaskSchedule(t.ID, t.ScheduleJSON, next); err != nil {
			return err
		}
	}

	res := s.Chat.SendScheduledWake(agentID, wake.String())
	if !res.Ok {
		code := ""
		if res.Error != nil {
			code = res.Error.Code
		}
		if code == string(Shared.CodeBusy) {
			// Nothing was started and the agent will free up shortly: keep the
			// task due, pause the whole engine briefly, do not count it as a
			// failure of this schedule.
			s.mu.Lock()
			s.lastBusyAt = time.Now()
			s.mu.Unlock()
			return fmt.Errorf("agent busy, will retry")
		}
		msg := "send failed"
		if res.Error != nil {
			msg = res.Error.Message
		}
		// Record the attempt so list_scheduled shows why nothing arrived, and
		// keep the task due — a reminder that never started is still owed.
		if rid, uerr := uuid.NewV7(); uerr == nil {
			if err := s.Core.DB.InsertScheduledRun(Storage.ScheduledRun{
				ID:          rid.String(),
				TaskID:      t.ID,
				AgentID:     agentID,
				Status:      Storage.RunError,
				Error:       msg,
				StartedAt:   now,
				CompletedAt: time.Now().UnixMilli(),
			}); err != nil {
				log.Printf("scheduler: could not record failed attempt for %s: %v", t.ID, err)
			}
		}
		_ = s.Core.DB.MarkTaskFired(t.ID, now, msg, false)
		wait := s.noteFailure(t.ID, time.Now())
		log.Printf("scheduler: %s failed (%s) — retrying in %s", t.Title, msg, wait)
		return nil
	}
	s.clearFailure(t.ID)

	rid, uerr := uuid.NewV7()
	if uerr != nil {
		return uerr
	}
	runRowID := rid.String()
	if err := s.Core.DB.InsertScheduledRun(Storage.ScheduledRun{
		ID:        runRowID,
		TaskID:    t.ID,
		AgentID:   agentID,
		MessageID: res.AssistantMessageID,
		Status:    Storage.RunStarted,
		StartedAt: now,
	}); err != nil {
		return err
	}
	// Reminders retire the moment their run has been started for real;
	// routines already advanced above.
	if t.Kind == Storage.KindReminder {
		if err := s.Core.DB.MarkTaskFired(t.ID, now, "", true); err != nil {
			return err
		}
	}
	s.mu.Lock()
	if s.pending == nil {
		s.pending = map[string]pendingRun{}
	}
	s.pending[res.RunID] = pendingRun{taskID: t.ID, runID: runRowID, title: t.Title}
	s.mu.Unlock()
	log.Printf("scheduler: fired %s %q (late %s)", kindLabel, t.Title, humanDelay(late))
	return nil
}

// nextFire computes the following occurrence. The anchor is the fire we are
// handling, so interval schedules keep their cadence across restarts.
func (s *Service) nextFire(sp spec.Schedule, afterMS int64, t Storage.ScheduledTask) (int64, error) {
	anchor := t.NextRunAt
	if anchor <= 0 {
		anchor = t.CreatedAt
	}
	return sp.NextAfter(afterMS, anchor, time.Local)
}

// runFinished closes the loop when the agent run a schedule started reaches
// its final state: finalize the run row, refresh last_status, notify.
func (s *Service) runFinished(out servicechat.RunOutcome) {
	s.mu.Lock()
	p, ok := s.pending[out.RunID]
	delete(s.pending, out.RunID)
	s.mu.Unlock()
	if !ok {
		return
	}
	status, detail := Storage.RunSuccess, ""
	switch out.Status {
	case "success":
	default:
		status = Storage.RunError
		if out.Status == "cancelled" {
			detail = "cancelled before it finished"
		} else {
			detail = "the agent run failed"
		}
	}
	done := time.Now().UnixMilli()
	if err := s.Core.DB.FinishScheduledRun(p.runID, status, out.AssistantMessageID, detail, done); err != nil {
		log.Printf("scheduler: could not finalize run %s: %v", p.runID, err)
	}
	if err := s.Core.DB.MarkTaskFired(p.taskID, done, status, false); err != nil {
		log.Printf("scheduler: could not mark task %s fired: %v", p.taskID, err)
	}
	// The row now carries the run's outcome, which is what the panel shows.
	s.notifyChanged()
	body := "The scheduled run finished."
	if msg, found, err := s.Core.DB.GetMessage(out.AssistantMessageID); err == nil && found {
		if line := firstLine(msg.Text); line != "" {
			body = line
		}
	}
	if detail != "" {
		body = fmt.Sprintf("%s (%s)", body, detail)
	}
	if s.Notifier != nil {
		s.Notifier.Notify(p.title, body)
	}
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	line = strings.TrimSpace(line)
	if r := []rune(line); len(r) > 160 {
		line = string(r[:160]) + "…"
	}
	return line
}

// lateLate is the qualifier a fire's lateness earns: within a minute is a
// rounding artefact worth naming, anything past that stands on its own.
func lateLate(lateMs int64) string {
	if lateMs < 60_000 {
		return ", under a minute, late"
	}
	return " late"
}

func humanDelay(ms int64) string {
	if ms <= 0 {
		return "0s"
	}
	d := time.Duration(ms) * time.Millisecond
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}

// online reports whether the machine has network, cached briefly so a
// multi-task sweep doesn't pay the probe per fire.
func (s *Service) online() bool {
	s.onlineMu.Lock()
	defer s.onlineMu.Unlock()
	if !s.onlineAt.IsZero() && time.Since(s.onlineAt) < onlineCache {
		return s.onlineOK
	}
	s.onlineAt = time.Now()
	s.onlineOK = probe()
	return s.onlineOK
}

func probe() bool {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, "https://ipwho.is/", nil)
	if err != nil {
		return false
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	return res.StatusCode < 500
}
