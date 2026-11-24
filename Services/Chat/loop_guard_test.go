package Chat

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestIsQuotaRateLimitError(t *testing.T) {
	matches := []string{
		"googleapi: Error 429: daily_limit_reached",
		"HTTP 429 Too Many Requests",
		"rate limit exceeded for tool",
		"quota exceeded for project",
		"RESOURCE_EXHAUSTED: Quota failure",
	}
	for _, m := range matches {
		if !isQuotaRateLimitError(m) {
			t.Fatalf("expected match: %q", m)
		}
	}
	misses := []string{
		"file not found: /tmp/x",
		"invalid arguments for tool",
		"connection refused",
		"error 403 forbidden",
		"the 4301 document id is invalid",
	}
	for _, m := range misses {
		if isQuotaRateLimitError(m) {
			t.Fatalf("unexpected match: %q", m)
		}
	}
}

// failingTurn varies the tool input every round — the pattern that evaded the
// identical-signature stuck detector in the Gmail incident.
func failingTurn(round int) ([]ToolCall, error) {
	return []ToolCall{{Name: "gmail__search_messages", Args: fmt.Sprintf(`{"query":"attempt %d"}`, round)}}, nil
}

func TestRunAgentLoopStopsOnNoProgressDespiteVaryingCalls(t *testing.T) {
	execCount := 0
	turnCount := 0
	cfg := LoopConfig{
		AgentID:   "a1",
		ThreadID:  "t1",
		MaxRounds: 250,
		Turn: func(ctx context.Context, cfg LoopConfig, messages []LoopMessage) (ModelResponse, error) {
			turnCount++
			calls, _ := failingTurn(turnCount)
			return ModelResponse{ToolCalls: calls}, nil
		},
		Exec: func(ctx context.Context, cfg LoopConfig, call ToolCall) (string, error) {
			execCount++
			return "", errors.New("tool not found")
		},
	}
	_, err := RunAgentLoop(context.Background(), nil, cfg)
	if err != nil {
		t.Fatalf("loop returned error: %v", err)
	}
	if execCount != noProgressRoundLimit {
		t.Fatalf("expected %d tool calls before no-progress stop, got %d", noProgressRoundLimit, execCount)
	}
	if turnCount > noProgressRoundLimit+1 {
		t.Fatalf("loop kept turning after no progress: %d turns", turnCount)
	}
}

func TestRunAgentLoopStopsOnToolCallBudget(t *testing.T) {
	execCount := 0
	turnCount := 0
	batch := 10
	cfg := LoopConfig{
		AgentID:   "a1",
		ThreadID:  "t1",
		MaxRounds: 250,
		Turn: func(ctx context.Context, cfg LoopConfig, messages []LoopMessage) (ModelResponse, error) {
			turnCount++
			calls := make([]ToolCall, batch)
			for i := range calls {
				calls[i] = ToolCall{Name: fmt.Sprintf("tool_%d_%d", turnCount, i), Args: `{}`}
			}
			return ModelResponse{ToolCalls: calls}, nil
		},
		Exec: func(ctx context.Context, cfg LoopConfig, call ToolCall) (string, error) {
			execCount++
			return "ok", nil
		},
	}
	_, err := RunAgentLoop(context.Background(), nil, cfg)
	if err != nil {
		t.Fatalf("loop returned error: %v", err)
	}
	if execCount < maxToolCallsPerTurn {
		t.Fatalf("stopped before the budget: %d < %d", execCount, maxToolCallsPerTurn)
	}
	if execCount > maxToolCallsPerTurn+batch {
		t.Fatalf("loop overshot the budget badly: %d calls", execCount)
	}
	if turnCount > maxToolCallsPerTurn/batch+1 {
		t.Fatalf("too many rounds: %d", turnCount)
	}
}

func TestRunAgentLoopStopsOnQuotaAfterOneRetry(t *testing.T) {
	execCount := 0
	cfg := LoopConfig{
		AgentID:   "a1",
		ThreadID:  "t1",
		MaxRounds: 250,
		Turn: func(ctx context.Context, cfg LoopConfig, messages []LoopMessage) (ModelResponse, error) {
			return ModelResponse{ToolCalls: []ToolCall{{Name: "gmail__list_messages", Args: `{}`}}}, nil
		},
		Exec: func(ctx context.Context, cfg LoopConfig, call ToolCall) (string, error) {
			execCount++
			return "", errors.New("429 daily_limit_reached")
		},
	}
	_, err := RunAgentLoop(context.Background(), nil, cfg)
	if err != nil {
		t.Fatalf("loop returned error: %v", err)
	}
	if execCount != 2 {
		t.Fatalf("quota error must stop the run after one retry, got %d exec calls", execCount)
	}
}

func TestRunAgentLoopCompletesWhenModelStopsCallingTools(t *testing.T) {
	turnCount := 0
	cfg := LoopConfig{
		AgentID:   "a1",
		ThreadID:  "t1",
		MaxRounds: 250,
		Turn: func(ctx context.Context, cfg LoopConfig, messages []LoopMessage) (ModelResponse, error) {
			turnCount++
			if turnCount == 1 {
				return ModelResponse{ToolCalls: []ToolCall{{Name: "tool_a", Args: `{}`}}}, nil
			}
			return ModelResponse{Text: "done"}, nil
		},
		Exec: func(ctx context.Context, cfg LoopConfig, call ToolCall) (string, error) {
			return "ok", nil
		},
	}
	resp, err := RunAgentLoop(context.Background(), nil, cfg)
	if err != nil {
		t.Fatalf("loop returned error: %v", err)
	}
	if resp.Text != "done" {
		t.Fatalf("expected final response, got %q", resp.Text)
	}
	if turnCount != 2 {
		t.Fatalf("expected 2 turns, got %d", turnCount)
	}
}
