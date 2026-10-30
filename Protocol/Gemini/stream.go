package Gemini

import (
	"encoding/json"
	"errors"
	"io"
	"iter"
	"strconv"
	"strings"

	"github.com/anyscapelabs/argus/Protocol"
)

type wireChunk struct {
	Candidates     []wireCandidate `json:"candidates"`
	UsageMetadata  *wireUsage      `json:"usageMetadata,omitempty"`
	PromptFeedback *wireFeedback   `json:"promptFeedback,omitempty"`
	Error          *wireError      `json:"error,omitempty"`
}

type wireCandidate struct {
	Content      *wireContent `json:"content,omitempty"`
	FinishReason string       `json:"finishReason,omitempty"`
}

type wireFeedback struct {
	BlockReason string `json:"blockReason,omitempty"`
}

type wireUsage struct {
	PromptTokenCount     int64 `json:"promptTokenCount"`
	CandidatesTokenCount int64 `json:"candidatesTokenCount"`
	ThoughtsTokenCount   int64 `json:"thoughtsTokenCount"`
	TotalTokenCount      int64 `json:"totalTokenCount"`
	CachedTokenCount     int64 `json:"cachedContentTokenCount"`
}

func (w *wireUsage) Canonical() Protocol.Usage {
	input := w.PromptTokenCount - w.CachedTokenCount
	if input < 0 {
		input = 0
	}
	return Protocol.Usage{
		InputTokens:     input,
		OutputTokens:    w.CandidatesTokenCount,
		CacheReadTokens: w.CachedTokenCount,
		ReasoningTokens: w.ThoughtsTokenCount,
	}
}

type wireError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

type streamState struct {
	msgStarted bool
	nextBlock  int
	textOpen   bool
	textIdx    int
	thinkOpen  bool
	thinkIdx   int
	toolSeq    int
	usage      Protocol.Usage
	sawTool    bool
	sawText    bool
	finish     string
	stopped    bool
}

func (a *Adapter) ParseStream(body io.Reader) iter.Seq[Protocol.Event] {
	return func(yield func(Protocol.Event) bool) {
		st := &streamState{}
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
			var chunk wireChunk
			if err := json.Unmarshal([]byte(frame.Data), &chunk); err != nil {
				g.fail(Protocol.NewError(Protocol.CodeBadResponse, "The provider sent an unparseable stream chunk"))
				return
			}
			if !st.step(chunk, g) {
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

func (st *streamState) step(chunk wireChunk, g *gateway) bool {
	if chunk.Error != nil {
		g.fail(chunkError(chunk.Error))
		return false
	}
	if !st.msgStarted {
		st.msgStarted = true
		if !g.emit(Protocol.MessageStartEvent(nil)) {
			return false
		}
	}
	if chunk.UsageMetadata != nil {
		u := chunk.UsageMetadata.Canonical()
		st.usage.Merge(&u)
	}
	if chunk.PromptFeedback != nil && chunk.PromptFeedback.BlockReason != "" && !st.stopped {
		st.stopped = true
		if !st.close(g) {
			return false
		}
		stop := Protocol.Stop{Reason: Protocol.StopContentFilter, Raw: chunk.PromptFeedback.BlockReason}
		if !g.emit(Protocol.MessageDeltaEvent(&stop, &st.usage)) {
			return false
		}
		g.emit(Protocol.MessageStopEvent())
		g.ok = false
		return true
	}
	for _, cand := range chunk.Candidates {
		if cand.Content != nil {
			for _, part := range cand.Content.Parts {
				if !st.part(part, g) {
					return false
				}
			}
		}
		if cand.FinishReason != "" {
			st.finish = cand.FinishReason
		}
	}
	return true
}

func (st *streamState) part(p wirePart, g *gateway) bool {
	if p.FunctionResponse != nil {
		return true
	}
	if p.FunctionCall != nil {
		if !st.close(g) {
			return false
		}
		st.sawTool = true
		idx := st.nextBlock
		st.nextBlock++
		id := p.FunctionCall.ID
		if id == "" {
			id = "synth_" + itoa(st.toolSeq)
			st.toolSeq++
		}
		input := strings.TrimSpace(string(p.FunctionCall.Args))
		valid := input == "" || json.Valid([]byte(input))
		block := Protocol.Block{Type: Protocol.BlockToolUse, ToolCall: &Protocol.ToolCall{ID: id, Name: p.FunctionCall.Name, Input: input, Valid: valid}}
		if p.ThoughtSignature != "" {
			block.Extra, _ = json.Marshal(map[string]string{"thoughtSignature": p.ThoughtSignature})
		}
		if !g.emit(Protocol.BlockStartEvent(idx, block)) {
			return false
		}
		return g.emit(Protocol.BlockStopEvent(idx, block))
	}
	if p.Thought {
		if p.Text == "" {
			return true
		}
		if !st.open(Protocol.BlockThinking, g) {
			return false
		}
		if p.Text != "" && !g.emit(Protocol.DeltaEvent(st.thinkIdx, Protocol.Delta{Kind: Protocol.DeltaThinking, Text: p.Text})) {
			return false
		}
		if p.ThoughtSignature != "" && !g.emit(Protocol.DeltaEvent(st.thinkIdx, Protocol.Delta{Kind: Protocol.DeltaSignature, Signature: p.ThoughtSignature})) {
			return false
		}
		return true
	}
	if p.Text == "" {
		return true
	}
	st.sawText = true
	if !st.open(Protocol.BlockText, g) {
		return false
	}
	return g.emit(Protocol.DeltaEvent(st.textIdx, Protocol.Delta{Kind: Protocol.DeltaText, Text: p.Text}))
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func (st *streamState) open(typ Protocol.BlockType, g *gateway) bool {
	if typ == Protocol.BlockText {
		if st.textOpen {
			return true
		}
		st.textIdx = st.nextBlock
		st.nextBlock++
		st.textOpen = true
		return g.emit(Protocol.BlockStartEvent(st.textIdx, Protocol.Block{Type: Protocol.BlockText}))
	}
	if st.thinkOpen {
		return true
	}
	st.thinkIdx = st.nextBlock
	st.nextBlock++
	st.thinkOpen = true
	return g.emit(Protocol.BlockStartEvent(st.thinkIdx, Protocol.Block{Type: Protocol.BlockThinking}))
}

func (st *streamState) close(g *gateway) bool {
	if st.textOpen {
		st.textOpen = false
		if !g.emit(Protocol.BlockStopEvent(st.textIdx, Protocol.Block{Type: Protocol.BlockText})) {
			return false
		}
	}
	if st.thinkOpen {
		st.thinkOpen = false
		if !g.emit(Protocol.BlockStopEvent(st.thinkIdx, Protocol.Block{Type: Protocol.BlockThinking})) {
			return false
		}
	}
	return true
}

func (st *streamState) finalize(g *gateway) {
	if st.stopped {
		return
	}
	if !st.close(g) {
		return
	}
	if !st.msgStarted && st.finish == "" && !st.sawText && !st.sawTool {
		g.fail(Protocol.NewError(Protocol.CodeBadResponse, "The provider sent an empty stream"))
		return
	}
	stop := stopFor(st.finish, st.sawTool)
	if !g.emit(Protocol.MessageDeltaEvent(stop, &st.usage)) {
		return
	}
	g.emit(Protocol.MessageStopEvent())
	g.ok = false
}

func stopFor(finish string, sawTool bool) *Protocol.Stop {
	var reason Protocol.StopReason
	raw := finish
	switch finish {
	case "STOP", "":
		if sawTool {
			reason = Protocol.StopToolUse
		} else {
			reason = Protocol.StopEndTurn
		}
	case "MAX_TOKENS":
		reason = Protocol.StopMaxTokens
	case "SAFETY", "PROHIBITED_CONTENT", "IMAGE_SAFETY", "BLOCKLIST", "SPII", "RECITATION":
		reason = Protocol.StopContentFilter
	case "tool_use":
		reason = Protocol.StopToolUse
	case "MALFORMED_FUNCTION_CALL":
		reason = Protocol.StopOther
	default:
		reason = Protocol.StopOther
	}
	return &Protocol.Stop{Reason: reason, Raw: raw}
}

func chunkError(e *wireError) *Protocol.Error {
	return &Protocol.Error{
		Code:    mapStatusCode(e.Code, e.Status),
		Message: strings.TrimSpace(e.Message),
		Raw:     e.Status,
	}
}
