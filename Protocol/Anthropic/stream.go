package Anthropic

import (
	"encoding/json"
	"errors"
	"io"
	"iter"
	"strings"

	"github.com/anyscapelabs/argus/Protocol"
)

type wireEventFrame struct {
	Type         string           `json:"type"`
	Index        *int             `json:"index,omitempty"`
	Message      *wireMessageHead `json:"message,omitempty"`
	Usage        *wireUsageFields `json:"usage,omitempty"`
	ContentBlock *wireStartBlock  `json:"content_block,omitempty"`
	Delta        *wireEventDelta  `json:"delta,omitempty"`
	Error        *wireError       `json:"error,omitempty"`
}

type wireMessageHead struct {
	ID    string          `json:"id"`
	Role  string          `json:"role"`
	Model string          `json:"model"`
	Usage wireUsageFields `json:"usage"`
}

type wireUsageFields struct {
	InputTokens              int64 `json:"input_tokens"`
	CacheCreation            any   `json:"cache_creation"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
}

type wireStartBlock struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
	Text string `json:"text"`
}

type wireEventDelta struct {
	Type        string          `json:"type"`
	Text        string          `json:"text"`
	Thinking    string          `json:"thinking"`
	Signature   string          `json:"signature"`
	PartialJSON string          `json:"partial_json"`
	StopReason  string          `json:"stop_reason"`
	StopSeq     string          `json:"stop_sequence"`
	Usage       wireUsageFields `json:"usage"`
}

type wireError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type streamState struct {
	msgStarted bool
	blocks     map[int]wireStartBlock
	toolBuf    map[int]*strings.Builder
	nextUsage  Protocol.Usage
	stopReason string
	stopSeq    string
	stopped    bool
}

func (a *Adapter) ParseStream(body io.Reader) iter.Seq[Protocol.Event] {
	return func(yield func(Protocol.Event) bool) {
		st := &streamState{blocks: map[int]wireStartBlock{}, toolBuf: map[int]*strings.Builder{}}
		g := &gateway{yield: yield, ok: true}
		frames := Protocol.NewFrames(body)
		for g.ok {
			frame, err := frames.Next()
			if err != nil {
				if errors.Is(err, io.EOF) {
					st.finalize(g)
					return
				}
				g.fail(Protocol.StreamEndingError(err))
				return
			}
			var ev wireEventFrame
			if err := json.Unmarshal([]byte(frame.Data), &ev); err != nil {
				g.fail(Protocol.NewError(Protocol.CodeBadResponse, "The provider sent an unparseable stream event"))
				return
			}
			if !st.step(ev, g) {
				return
			}
		}
	}
}

type gateway struct {
	yield func(Protocol.Event) bool
	ok    bool
}

func (g *gateway) emit(ev Protocol.Event) bool {
	if !g.ok {
		return false
	}
	if !g.yield(ev) {
		g.ok = false
		return false
	}
	return true
}

func (g *gateway) fail(err *Protocol.Error) bool {
	if !g.ok {
		return false
	}
	g.emit(Protocol.ErrorEvent(err))
	g.ok = false
	return false
}

func (st *streamState) step(ev wireEventFrame, g *gateway) bool {
	if ev.Error != nil && ev.Type == "error" {
		g.fail(chunkError(ev.Error))
		return false
	}
	if ev.Type == "ping" {
		return true
	}
	switch ev.Type {
	case "message_start":
		st.msgStarted = true
		if ev.Message != nil {
			u := ev.Message.Usage.Canonical()
			st.nextUsage.Merge(&u)
		}
		usage := st.nextUsage
		return g.emit(Protocol.MessageStartEvent(&usage))
	case "content_block_start":
		if ev.Index == nil || ev.ContentBlock == nil {
			return true
		}
		idx := *ev.Index
		st.blocks[idx] = *ev.ContentBlock
		block := startBlock(*ev.ContentBlock)
		if block.Type == Protocol.BlockToolUse {
			st.toolBuf[idx] = &strings.Builder{}
		}
		return g.emit(Protocol.BlockStartEvent(idx, block))
	case "content_block_delta":
		if ev.Index == nil || ev.Delta == nil {
			return true
		}
		idx := *ev.Index
		d := ev.Delta
		if d.Usage.OutputTokens > 0 {
			st.nextUsage.OutputTokens = d.Usage.OutputTokens
		}
		if d.StopReason != "" {
			st.stopReason = d.StopReason
		}
		if d.StopSeq != "" {
			st.stopSeq = d.StopSeq
		}
		kind, text, ok := deltaFor(d)
		if !ok {
			return true
		}
		if kind == Protocol.DeltaToolJSON {
			if tb, isTool := st.toolBuf[idx]; isTool {
				tb.WriteString(text)
			}
		}
		return g.emit(Protocol.DeltaEvent(idx, Protocol.Delta{Kind: kind, Text: text, Signature: d.Signature}))
	case "content_block_stop":
		if ev.Index == nil {
			return true
		}
		idx := *ev.Index
		start, exists := st.blocks[idx]
		if tb, isTool := st.toolBuf[idx]; isTool {
			delete(st.blocks, idx)
			delete(st.toolBuf, idx)
			input := strings.TrimSpace(tb.String())
			valid := true
			if input != "" {
				valid = json.Valid([]byte(input))
			}
			call := Protocol.ToolCall{ID: start.ID, Name: start.Name, Input: input, Valid: valid}
			return g.emit(Protocol.BlockStopEvent(idx, Protocol.Block{Type: Protocol.BlockToolUse, ToolCall: &call}))
		}
		final := Protocol.Block{Type: Protocol.BlockText}
		if exists {
			final = startBlock(start)
		}
		delete(st.blocks, idx)
		return g.emit(Protocol.BlockStopEvent(idx, final))
	case "message_delta":
		if ev.Delta != nil {
			if ev.Delta.StopReason != "" {
				st.stopReason = ev.Delta.StopReason
			}
			if ev.Delta.StopSeq != "" {
				st.stopSeq = ev.Delta.StopSeq
			}
		}
		if ev.Usage != nil {
			newer := ev.Usage.Canonical()
			st.nextUsage.Merge(&newer)
		}
		usage := st.nextUsage
		return g.emit(Protocol.MessageDeltaEvent(stopFor(st.stopReason, st.stopSeq), &usage))
	case "message_stop":
		if !st.msgStarted && st.stopReason == "" && st.nextUsage.InputTokens == 0 && st.nextUsage.OutputTokens == 0 {
			return g.fail(Protocol.NewError(Protocol.CodeBadResponse, "The provider sent an empty stream"))
		}
		st.stopped = true
		if !g.emit(Protocol.MessageStopEvent()) {
			return false
		}
		g.ok = false
		return true
	case "error":
		g.fail(Protocol.NewError(Protocol.CodeProviderError, "The provider stream failed"))
		return false
	}
	return true
}

func (st *streamState) finalize(g *gateway) {
	if st.stopped {
		return
	}
	if !st.msgStarted && st.stopReason == "" && st.nextUsage.InputTokens == 0 && st.nextUsage.OutputTokens == 0 {
		g.fail(Protocol.NewError(Protocol.CodeBadResponse, "The provider sent an empty stream"))
		return
	}
	if !g.emit(Protocol.MessageDeltaEvent(stopFor(st.stopReason, st.stopSeq), &st.nextUsage)) {
		return
	}
	g.emit(Protocol.MessageStopEvent())
	g.ok = false
}

type wireEnvelopeError struct {
	Error wireError `json:"error"`
}

func chunkError(e *wireError) *Protocol.Error {
	err := &Protocol.Error{
		Code:    mapErrorType(e.Type),
		Message: strings.TrimSpace(e.Message),
		Raw:     e.Type,
	}
	if err.Message == "" {
		err.Message = "The provider returned an error"
	}
	return err
}

func mapErrorType(t string) string {
	switch t {
	case "invalid_request_error":
		return Protocol.CodeBadInput
	case "authentication_error", "permission_error":
		return Protocol.CodeInvalidKey
	case "rate_limit_error":
		return Protocol.CodeRateLimited
	case "not_found_error":
		return Protocol.CodeBadInput
	case "overloaded_error":
		return Protocol.CodeProviderError
	case "api_error":
		return Protocol.CodeProviderError
	}
	return Protocol.CodeProviderError
}

func startBlock(w wireStartBlock) Protocol.Block {
	switch w.Type {
	case "text":
		return Protocol.Block{Type: Protocol.BlockText, Text: w.Text}
	case "thinking":
		return Protocol.Block{Type: Protocol.BlockThinking, Text: w.Text}
	case "tool_use":
		return Protocol.Block{Type: Protocol.BlockToolUse, ToolCall: &Protocol.ToolCall{ID: w.ID, Name: w.Name}}
	}
	return Protocol.Block{Type: Protocol.BlockText}
}

func deltaFor(d *wireEventDelta) (Protocol.DeltaKind, string, bool) {
	switch d.Type {
	case "text_delta":
		return Protocol.DeltaText, d.Text, true
	case "thinking_delta":
		return Protocol.DeltaThinking, d.Thinking, true
	case "signature_delta":
		if d.Signature == "" {
			return "", "", false
		}
		return Protocol.DeltaSignature, d.Signature, true
	case "input_json_delta":
		if d.PartialJSON == "" {
			return "", "", false
		}
		return Protocol.DeltaToolJSON, d.PartialJSON, true
	}
	return "", "", false
}

func (u wireUsageFields) Canonical() Protocol.Usage {
	cacheWrite := u.CacheCreationInputTokens
	if cacheWrite == 0 {
		cacheWrite = extractCacheWrite(u.CacheCreation)
	}
	return Protocol.Usage{
		InputTokens:      u.InputTokens,
		OutputTokens:     u.OutputTokens,
		CacheReadTokens:  u.CacheReadInputTokens,
		CacheWriteTokens: cacheWrite,
	}
}

func extractCacheWrite(raw any) int64 {
	switch v := raw.(type) {
	case float64:
		if v > 0 {
			return int64(v)
		}
	case map[string]any:
		total := int64(0)
		for _, val := range v {
			if n, ok := val.(float64); ok && n > 0 {
				total += int64(n)
			}
		}
		return total
	}
	return 0
}

func stopFor(reason string, sequence string) *Protocol.Stop {
	var mapped Protocol.StopReason
	switch reason {
	case "end_turn", "stop_sequence":
		mapped = Protocol.StopEndTurn
		if sequence != "" {
			mapped = Protocol.StopSequence
		}
	case "tool_use":
		mapped = Protocol.StopToolUse
	case "max_tokens":
		mapped = Protocol.StopMaxTokens
	case "refusal", "content_filter":
		mapped = Protocol.StopContentFilter
	case "":
		return nil
	default:
		mapped = Protocol.StopOther
	}
	return &Protocol.Stop{Reason: mapped, Raw: reason}
}
