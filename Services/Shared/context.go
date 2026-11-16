package Shared

import "context"

// unattendedKey marks a context as running without anyone watching — a
// scheduled fire while the app sits idle. The permission gate refuses
// interactive asks instead of blocking on them (a stored grant still wins),
// and anything else that would need a human can check the same flag.
type unattendedKey struct{}
type scheduledFireKey struct{}
type agentRunKey struct{}

type AgentRunContext struct {
	ThreadID  string
	Hops      int
	Delegated bool
}

func WithUnattended(ctx context.Context) context.Context {
	return context.WithValue(ctx, unattendedKey{}, true)
}

func IsUnattended(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(unattendedKey{}).(bool)
	return v
}

// WithScheduledFire marks the internal turn produced by a due reminder or
// routine. Scheduling tools use it as a hard guard against recursively creating
// another schedule from the task that is already firing.
func WithScheduledFire(ctx context.Context) context.Context {
	return context.WithValue(ctx, scheduledFireKey{}, true)
}

func IsScheduledFire(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(scheduledFireKey{}).(bool)
	return v
}

func WithAgentRun(ctx context.Context, run AgentRunContext) context.Context {
	return context.WithValue(ctx, agentRunKey{}, run)
}

func AgentRun(ctx context.Context) AgentRunContext {
	if ctx == nil {
		return AgentRunContext{}
	}
	run, _ := ctx.Value(agentRunKey{}).(AgentRunContext)
	return run
}
