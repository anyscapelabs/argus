package Protocol

import (
	"encoding/json"
)

type EventType string

const (
	EventMessageStart EventType = "message_start"
	EventBlockStart   EventType = "block_start"
	EventBlockDelta   EventType = "block_delta"
	EventBlockStop    EventType = "block_stop"
	EventMessageDelta EventType = "message_delta"
	EventMessageStop  EventType = "message_stop"
	EventError        EventType = "error"
)

type StopReason string

const (
	StopEndTurn       StopReason = "end_turn"
	StopToolUse       StopReason = "tool_use"
	StopMaxTokens     StopReason = "max_tokens"
	StopSequence      StopReason = "stop_sequence"
	StopContentFilter StopReason = "content_filter"
	StopOther         StopReason = "other"
)

type Stop struct {
	Reason StopReason `json:"reason"`
	Raw    string     `json:"raw,omitempty"`
}

type Usage struct {
	InputTokens      int64 `json:"input_tokens,omitempty"`
	OutputTokens     int64 `json:"output_tokens,omitempty"`
	CacheReadTokens  int64 `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64 `json:"cache_write_tokens,omitempty"`
	ReasoningTokens  int64 `json:"reasoning_tokens,omitempty"`
}

func (u *Usage) Merge(other *Usage) {
	if other == nil {
		return
	}
	if other.InputTokens != 0 {
		u.InputTokens = other.InputTokens
	}
	if other.OutputTokens != 0 {
		u.OutputTokens = other.OutputTokens
	}
	if other.CacheReadTokens != 0 {
		u.CacheReadTokens = other.CacheReadTokens
	}
	if other.CacheWriteTokens != 0 {
		u.CacheWriteTokens = other.CacheWriteTokens
	}
	if other.ReasoningTokens != 0 {
		u.ReasoningTokens = other.ReasoningTokens
	}
}

type DeltaKind string

const (
	DeltaText      DeltaKind = "text"
	DeltaThinking  DeltaKind = "thinking"
	DeltaSignature DeltaKind = "signature"
	DeltaToolJSON  DeltaKind = "tool_input"
)

type Delta struct {
	Kind      DeltaKind `json:"kind"`
	Text      string    `json:"text,omitempty"`
	Signature string    `json:"signature,omitempty"`
}

type BlockStart struct {
	Index int   `json:"index"`
	Block Block `json:"block"`
}

type Error struct {
	Code       string          `json:"code"`
	Message    string          `json:"message"`
	Retryable  bool            `json:"retryable"`
	RetryAfter int64           `json:"retry_after,omitempty"`
	Status     int             `json:"status,omitempty"`
	Raw        string          `json:"raw,omitempty"`
	Extra      json.RawMessage `json:"extra,omitempty"`
}

func (e *Error) Error() string {
	return e.Message
}

type Event struct {
	Type  EventType   `json:"type"`
	Index int         `json:"index"`
	Block *Block      `json:"block,omitempty"`
	Start *BlockStart `json:"start,omitempty"`
	Delta *Delta      `json:"delta,omitempty"`
	Stop  *Stop       `json:"stop,omitempty"`
	Usage *Usage      `json:"usage,omitempty"`
	Error *Error      `json:"error,omitempty"`
}

func ErrorEvent(err *Error) Event {
	return Event{Type: EventError, Error: err}
}

func BlockStartEvent(index int, b Block) Event {
	return Event{Type: EventBlockStart, Index: index, Start: &BlockStart{Index: index, Block: b.Clone()}, Block: b.clonePtr()}
}

func (b Block) clonePtr() *Block {
	c := b.Clone()
	return &c
}

func BlockStopEvent(index int, b Block) Event {
	return Event{Type: EventBlockStop, Index: index, Block: b.clonePtr()}
}

func DeltaEvent(index int, d Delta) Event {
	return Event{Type: EventBlockDelta, Index: index, Delta: &d}
}

func MessageDeltaEvent(stop *Stop, usage *Usage) Event {
	return Event{Type: EventMessageDelta, Stop: stop, Usage: usage}
}

func MessageStopEvent() Event {
	return Event{Type: EventMessageStop}
}

func MessageStartEvent(usage *Usage) Event {
	return Event{Type: EventMessageStart, Usage: usage}
}
