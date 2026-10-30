package Protocol

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func readAll(tb testing.TB, data string) []Frame {
	tb.Helper()
	frames := NewFrames(strings.NewReader(data))
	out := []Frame{}
	for {
		frame, err := frames.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			tb.Fatalf("frame reader: %v", err)
		}
		out = append(out, frame)
	}
}

func TestFrameEventsAndData(t *testing.T) {
	payload := "event: message_start\ndata: {\"a\":1}\n\nevent: message_stop\ndata: {}\n\n"
	frames := readAll(t, payload)
	if len(frames) != 2 {
		t.Fatalf("got %d frames, want 2", len(frames))
	}
	if frames[0].Event != "message_start" || frames[0].Data != "{\"a\":1}" {
		t.Fatalf("frame 0 = %+v", frames[0])
	}
	if frames[1].Event != "message_stop" {
		t.Fatalf("frame 1 = %+v", frames[1])
	}
}

func TestFrameDataOnly(t *testing.T) {
	payload := "data: one\n\ndata: two\n"
	frames := readAll(t, payload)
	if len(frames) != 2 || frames[0].Data != "one" || frames[1].Data != "two" {
		t.Fatalf("frames = %+v", frames)
	}
	if frames[0].Event != "" {
		t.Fatalf("event should be empty, got %q", frames[0].Event)
	}
}

func TestFrameMultiLineData(t *testing.T) {
	payload := "data: line1\ndata: line2\n\n"
	frames := readAll(t, payload)
	if len(frames) != 1 || frames[0].Data != "line1\nline2" {
		t.Fatalf("frames = %+v", frames)
	}
}

func TestFrameCommentsAndCarriageReturns(t *testing.T) {
	payload := ": keep alive ping\r\n\r\nevent: data\r\ndata: x\r\n"
	frames := readAll(t, payload)
	if len(frames) != 1 || frames[0].Event != "data" || frames[0].Data != "x" {
		t.Fatalf("frames = %+v", frames)
	}
}

func TestFramePendingDataAtEOF(t *testing.T) {
	payload := "data: trailing\n"
	frames := readAll(t, payload)
	if len(frames) != 1 || frames[0].Data != "trailing" {
		t.Fatalf("frames = %+v", frames)
	}
}

func TestFrameBlankLinesDoNotEmitEmpty(t *testing.T) {
	payload := "\n\n\ndata: after\n\n"
	frames := readAll(t, payload)
	if len(frames) != 1 || frames[0].Data != "after" {
		t.Fatalf("frames = %+v", frames)
	}
}

func TestCollectTextAndToolFlow(t *testing.T) {
	events := []Event{
		MessageStartEvent(&Usage{InputTokens: 5, OutputTokens: 0}),
		BlockStartEvent(0, Block{Type: BlockText}),
		DeltaEvent(0, Delta{Kind: DeltaText, Text: "hel"}),
		DeltaEvent(0, Delta{Kind: DeltaText, Text: "lo"}),
		BlockStopEvent(0, Block{Type: BlockText}),
		BlockStartEvent(1, Block{Type: BlockToolUse, ToolCall: &ToolCall{ID: "t1", Name: "calc"}}),
		DeltaEvent(1, Delta{Kind: DeltaToolJSON, Text: "{\"a\":"}),
		DeltaEvent(1, Delta{Kind: DeltaToolJSON, Text: "1}"}),
		BlockStopEvent(1, Block{Type: BlockToolUse, ToolCall: &ToolCall{ID: "t1", Name: "calc", Input: "{\"a\":1}", Valid: true}}),
		MessageDeltaEvent(&Stop{Reason: StopToolUse}, &Usage{OutputTokens: 7}),
		MessageStopEvent(),
	}
	res := Collect(func(y func(Event) bool) {
		for _, e := range events {
			if !y(e) {
				return
			}
		}
	})
	if res.Error != nil {
		t.Fatalf("unexpected error: %+v", res.Error)
	}
	if res.Stop.Reason != StopToolUse {
		t.Fatalf("stop = %+v", res.Stop)
	}
	if res.Usage.InputTokens != 5 || res.Usage.OutputTokens != 7 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	if len(res.Message.Blocks) != 2 || res.Message.Blocks[0].Text != "hello" {
		t.Fatalf("blocks = %+v", res.Message.Blocks)
	}
	call := res.Message.Blocks[1].ToolCall
	if call == nil || call.Input != "{\"a\":1}" || !call.Valid {
		t.Fatalf("tool call = %+v", call)
	}
}

func TestCollectMalformedToolJSON(t *testing.T) {
	events := []Event{
		MessageStartEvent(nil),
		BlockStartEvent(0, Block{Type: BlockToolUse, ToolCall: &ToolCall{ID: "t1", Name: "f"}}),
		DeltaEvent(0, Delta{Kind: DeltaToolJSON, Text: "{\"a\":"}),
		BlockStopEvent(0, Block{Type: BlockToolUse, ToolCall: &ToolCall{ID: "t1", Name: "f", Input: "{\"a\":", Valid: false}}),
		MessageDeltaEvent(&Stop{Reason: StopToolUse}, nil),
		MessageStopEvent(),
	}
	res := Collect(func(y func(Event) bool) {
		for _, e := range events {
			y(e)
		}
	})
	if res.Error != nil {
		t.Fatalf("unexpected error: %+v", res.Error)
	}
	call := res.Message.Blocks[0].ToolCall
	if call == nil || call.Valid || call.Input != "{\"a\":" {
		t.Fatalf("tool call = %+v", call)
	}
}

func TestCollectErrorTerminal(t *testing.T) {
	res := Collect(func(y func(Event) bool) {
		y(MessageStartEvent(nil))
		y(ErrorEvent(NewError(CodeRateLimited, "slow down")))
	})
	if res.Error == nil || res.Error.Code != CodeRateLimited {
		t.Fatalf("error = %+v", res.Error)
	}
}

func TestCollectUnexpectedEnd(t *testing.T) {
	res := Collect(func(y func(Event) bool) {
		y(MessageStartEvent(nil))
		y(MessageDeltaEvent(&Stop{Reason: StopEndTurn}, nil))
	})
	if res.Error == nil || res.Error.Code != CodeBadResponse {
		t.Fatalf("error = %+v", res.Error)
	}
}
