package OpenAI

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
	Error  *wireError   `json:"error"`
	Usage  *wireUsage   `json:"usage"`
	Choice []wireChoice `json:"choices"`
}

type wireChoice struct {
	Delta        wireDelta `json:"delta"`
	FinishReason string    `json:"finish_reason"`
}

type wireDelta struct {
	Content          any            `json:"content"`
	Reasoning        any            `json:"reasoning"`
	ReasoningContent string         `json:"reasoning_content"`
	ToolCalls        []wireFragment `json:"tool_calls"`
}

type wireFragment struct {
	Index    int         `json:"index"`
	ID       string      `json:"id"`
	Type     string      `json:"type"`
	Function wireToolFnc `json:"function"`
}

type wireToolFnc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type wireError struct {
	Message string          `json:"message"`
	Type    string          `json:"type"`
	Code    json.RawMessage `json:"code"`
	Param   string          `json:"param"`
}

type wireUsage struct {
	PromptTokens     int64                      `json:"prompt_tokens"`
	CompletionTokens int64                      `json:"completion_tokens"`
	PromptDetail     *wirePromptUsageDetails    `json:"prompt_tokens_details,omitempty"`
	CompletionDetail *wireCompletionUsageDetail `json:"completion_tokens_details,omitempty"`
}

type wirePromptUsageDetails struct {
	CachedTokens int64 `json:"cached_tokens"`
}

type wireCompletionUsageDetail struct {
	ReasoningTokens int64 `json:"reasoning_tokens"`
}

func (w *wireUsage) Canonical() Protocol.Usage {
	cacheRead := int64(0)
	if w.PromptDetail != nil {
		cacheRead = w.PromptDetail.CachedTokens
	}
	input := w.PromptTokens - cacheRead
	if input < 0 {
		input = 0
	}
	reasoning := int64(0)
	if w.CompletionDetail != nil {
		reasoning = w.CompletionDetail.ReasoningTokens
	}
	output := w.CompletionTokens - reasoning
	if output < 0 {
		output = 0
	}
	return Protocol.Usage{
		InputTokens:     input,
		OutputTokens:    output,
		CacheReadTokens: cacheRead,
		ReasoningTokens: reasoning,
	}
}

type toolState struct {
	idx      int
	blockIdx int
	id       string
	name     string
	buf      strings.Builder
}

type streamState struct {
	quirks     Quirks
	msgStarted bool
	sawDone    bool
	nextBlock  int
	textOpen   bool
	textIdx    int
	thinkOpen  bool
	thinkIdx   int
	tools      map[int]*toolState
	synthID    int
	finish     string
	usage      Protocol.Usage
}

func (a *Adapter) ParseStream(body io.Reader) iter.Seq[Protocol.Event] {
	return func(yield func(Protocol.Event) bool) {
		st := &streamState{quirks: a.quirks, tools: map[int]*toolState{}}
		g := &gateway{yield: yield, ok: true}
		frames := Protocol.NewFrames(body)
		for g.ok {
			frame, err := frames.Next()
			if err != nil {
				if errors.Is(err, io.EOF) {
					// A stream that ran out of frames without a finish reason
					// or the [DONE] sentinel was cut short mid-reply; saving
					// it as a complete message would drop the tail.
					if st.msgStarted && st.finish == "" && !st.sawDone {
						st.closeAllPartial(g)
						g.fail(Protocol.StreamEndingError(io.ErrUnexpectedEOF))
						return
					}
					st.finalize(g)
					return
				}
				g.fail(Protocol.StreamEndingError(err))
				return
			}
			if frame.Data == Protocol.DataDone {
				st.sawDone = true
				st.finalize(g)
				return
			}
			if !st.step(frame, g) {
				return
			}
		}
		if g.ok {
			st.finalize(g)
		}
	}
}

type gateway struct {
	yield func(Protocol.Event) bool
	ok    bool
}

func (g *gateway) init() { g.ok = true }

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

func (st *streamState) step(frame Protocol.Frame, g *gateway) bool {
	var chunk wireChunk
	if err := json.Unmarshal([]byte(frame.Data), &chunk); err != nil {
		g.fail(Protocol.NewError(Protocol.CodeBadResponse, "The provider sent an unparseable stream chunk"))
		return false
	}
	if chunk.Error != nil {
		g.fail(wireErrorToCanonical(chunk.Error))
		return false
	}
	if !st.msgStarted {
		st.msgStarted = true
		usage := st.usage
		if !g.emit(Protocol.MessageStartEvent(&usage)) {
			return false
		}
	}
	if chunk.Usage != nil {
		u := chunk.Usage.Canonical()
		st.usage.Merge(&u)
	}
	for i := range chunk.Choice {
		ch := chunk.Choice[i]
		if ch.FinishReason != "" && st.finish == "" {
			st.finish = ch.FinishReason
		}
		if !st.forward(st.quirks, ch.Delta, g) {
			return false
		}
	}
	return true
}

func (st *streamState) forward(q Quirks, d wireDelta, g *gateway) bool {
	if s, ok := d.Content.(string); ok && s != "" {
		if !st.open(Protocol.BlockText, g) {
			return false
		}
		if !g.emit(Protocol.DeltaEvent(st.textIdx, Protocol.Delta{Kind: Protocol.DeltaText, Text: s})) {
			return false
		}
	}
	reasoning := ""
	switch {
	case q.ReasoningField == "reasoning":
		if s, ok := d.Reasoning.(string); ok {
			reasoning = s
		}
	default:
		reasoning = d.ReasoningContent
	}
	if reasoning != "" {
		if !st.open(Protocol.BlockThinking, g) {
			return false
		}
		if !g.emit(Protocol.DeltaEvent(st.thinkIdx, Protocol.Delta{Kind: Protocol.DeltaThinking, Text: reasoning})) {
			return false
		}
	}
	for _, frag := range d.ToolCalls {
		ts, fresh := st.toolFor(frag)
		if fresh {
			if !g.emit(Protocol.BlockStartEvent(ts.blockIdx, toolStartBlock(ts))) {
				return false
			}
		}
		if frag.Function.Arguments != "" {
			ts.buf.WriteString(frag.Function.Arguments)
			if !g.emit(Protocol.DeltaEvent(ts.blockIdx, Protocol.Delta{Kind: Protocol.DeltaToolJSON, Text: frag.Function.Arguments})) {
				return false
			}
		}
	}
	return true
}

func toolStartBlock(ts *toolState) Protocol.Block {
	return Protocol.Block{Type: Protocol.BlockToolUse, ToolCall: &Protocol.ToolCall{ID: ts.id, Name: ts.name}}
}

func (st *streamState) toolFor(frag wireFragment) (*toolState, bool) {
	idx := frag.Index
	if ts, exists := st.tools[idx]; exists {
		if frag.ID != "" && frag.ID != ts.id {
			ts.id = frag.ID
		}
		if frag.Function.Name != "" && frag.Function.Name != ts.name {
			ts.name = frag.Function.Name
		}
		return ts, false
	}
	id := frag.ID
	if id == "" {
		id = "synth_" + strconv.Itoa(st.synthID) + "_" + strconv.Itoa(idx)
		st.synthID++
	}
	ts := &toolState{idx: idx, blockIdx: st.nextBlock, id: id, name: frag.Function.Name}
	st.nextBlock++
	st.tools[idx] = ts
	return ts, true
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

// closeAllPartial stops the open text/thinking/tool blocks without emitting
// message-level terminal events, so collected text survives a truncated
// stream instead of vanishing.
func (st *streamState) closeAllPartial(g *gateway) {
	if !g.ok {
		return
	}
	_ = st.closeAll(g)
}

func (st *streamState) finalize(g *gateway) {
	if !st.closeAll(g) {
		return
	}
	if !st.msgStarted && st.finish == "" && st.usage.InputTokens == 0 && st.usage.OutputTokens == 0 {
		g.fail(Protocol.NewError(Protocol.CodeBadResponse, "The provider sent an empty stream"))
		return
	}
	stop := stopFor(st.finish)
	if !g.emit(Protocol.MessageDeltaEvent(&stop, &st.usage)) {
		return
	}
	g.emit(Protocol.MessageStopEvent())
	g.ok = false
}

func (st *streamState) closeAll(g *gateway) bool {
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
	for _, ts := range st.tools {
		input := ts.buf.String()
		trimmed := strings.TrimSpace(input)
		valid := true
		if trimmed != "" {
			input = trimmed
			valid = json.Valid([]byte(trimmed))
		}
		call := Protocol.ToolCall{ID: ts.id, Name: ts.name, Input: input, Valid: valid}
		if !g.emit(Protocol.BlockStopEvent(ts.blockIdx, Protocol.Block{Type: Protocol.BlockToolUse, ToolCall: &call})) {
			return false
		}
	}
	st.tools = map[int]*toolState{}
	return true
}

func stopFor(finish string) Protocol.Stop {
	var reason Protocol.StopReason
	switch finish {
	case "stop", "":
		reason = Protocol.StopEndTurn
	case "function_call", "tool_calls", "tool_use":
		reason = Protocol.StopToolUse
	case "length":
		reason = Protocol.StopMaxTokens
	case "content_filter":
		reason = Protocol.StopContentFilter
	default:
		reason = Protocol.StopOther
	}
	return Protocol.Stop{Reason: reason, Raw: finish}
}

func wireErrorToCanonical(e *wireError) *Protocol.Error {
	err := &Protocol.Error{
		Code:    mapWireErrorType(e.Type),
		Message: firstNonEmpty(e.Message, "The provider returned an error"),
		Raw:     e.Type,
	}
	return err
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
