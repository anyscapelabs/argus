package Protocol

import (
	"encoding/json"
	"io"
	"iter"
)

type Result struct {
	Message Message `json:"message"`
	Usage   Usage   `json:"usage"`
	Stop    Stop    `json:"stop"`
	Error   *Error  `json:"error,omitempty"`
}

type collector struct {
	msg      Message
	usage    Usage
	lastStop Stop
	err      *Error
	tool     map[int]*toolBuf
}

type toolBuf struct {
	call ToolCall
	buf  []byte
}

func Collect(seq iter.Seq[Event]) Result {
	c := &collector{tool: map[int]*toolBuf{}}
	c.collect(seq)
	c.trim()
	return Result{Message: c.msg, Usage: c.usage, Stop: c.lastStop, Error: c.err}
}

func (c *collector) collect(seq iter.Seq[Event]) {
	done := false
	for ev := range seq {
		switch ev.Type {
		case EventMessageStart:
			c.usage.Merge(ev.Usage)
		case EventBlockStart:
			c.start(ev)
		case EventBlockDelta:
			c.delta(ev)
		case EventBlockStop:
			c.stopBlock(ev)
		case EventMessageDelta:
			if ev.Stop != nil {
				c.lastStop = *ev.Stop
			}
			c.usage.Merge(ev.Usage)
		case EventMessageStop:
			done = true
		case EventError:
			if ev.Error != nil {
				c.err = ev.Error
			} else {
				c.err = NewError(CodeBadResponse, "The model stream failed")
			}
		}
	}
	if c.err == nil && !done {
		c.err = StreamEndingError(io.EOF)
	}
}

func (c *collector) start(ev Event) {
	c.ensure(ev.Index)
	b := Block{}
	if ev.Start != nil {
		b = ev.Start.Block.Clone()
	} else if ev.Block != nil {
		b = ev.Block.Clone()
	}
	if b.Type == "" {
		b.Type = BlockText
	}
	c.msg.Blocks[ev.Index] = b
	if b.Type == BlockToolUse && b.ToolCall != nil {
		c.tool[ev.Index] = &toolBuf{call: *b.ToolCall}
	}
}

func (c *collector) delta(ev Event) {
	if ev.Delta == nil {
		return
	}
	c.ensure(ev.Index)
	switch ev.Delta.Kind {
	case DeltaText:
		c.msg.Blocks[ev.Index].Type = BlockText
		c.msg.Blocks[ev.Index].Text += ev.Delta.Text
	case DeltaThinking:
		c.msg.Blocks[ev.Index].Type = BlockThinking
		c.msg.Blocks[ev.Index].Text += ev.Delta.Text
	case DeltaSignature:
		c.msg.Blocks[ev.Index].Type = BlockThinking
		c.msg.Blocks[ev.Index].Signature += ev.Delta.Signature
	case DeltaToolJSON:
		c.msg.Blocks[ev.Index].Type = BlockToolUse
		if c.tool[ev.Index] == nil {
			c.tool[ev.Index] = &toolBuf{}
			if ev.Block != nil && ev.Block.ToolCall != nil {
				c.tool[ev.Index].call = *ev.Block.ToolCall
			} else if c.msg.Blocks[ev.Index].ToolCall != nil {
				c.tool[ev.Index].call = *c.msg.Blocks[ev.Index].ToolCall
			}
		}
		c.tool[ev.Index].buf = append(c.tool[ev.Index].buf, ev.Delta.Text...)
	}
}

func (c *collector) stopBlock(ev Event) {
	c.ensure(ev.Index)
	if ev.Block != nil && ev.Block.ToolCall != nil {
		c.msg.Blocks[ev.Index] = ev.Block.Clone()
		delete(c.tool, ev.Index)
		return
	}
	if t := c.tool[ev.Index]; t != nil {
		finalizeTool(&t.call, t.buf)
		if t.call.Name == "" && c.msg.Blocks[ev.Index].ToolName != "" {
			t.call.Name = c.msg.Blocks[ev.Index].ToolName
		}
		if t.call.ID == "" && c.msg.Blocks[ev.Index].ToolCall != nil && c.msg.Blocks[ev.Index].ToolCall.ID != "" {
			t.call.ID = c.msg.Blocks[ev.Index].ToolCall.ID
		}
		call := t.call
		c.msg.Blocks[ev.Index].Type = BlockToolUse
		c.msg.Blocks[ev.Index].ToolCall = &call
		delete(c.tool, ev.Index)
		return
	}
	if ev.Block != nil {
		final := ev.Block.Clone()
		cur := c.msg.Blocks[ev.Index]
		if final.Text == "" && (final.Type == BlockText || final.Type == BlockThinking) {
			final.Text = cur.Text
			final.Signature = cur.Signature
		}
		c.msg.Blocks[ev.Index] = final
	}
}

func (c *collector) ensure(index int) {
	for len(c.msg.Blocks) <= index {
		c.msg.Blocks = append(c.msg.Blocks, Block{})
	}
}

func finalizeTool(call *ToolCall, buf []byte) {
	trimmed := trimJSON(string(buf))
	if trimmed == "" {
		call.Input = ""
		call.Valid = true
		return
	}
	call.Input = trimmed
	call.Valid = json.Valid([]byte(trimmed))
}

func trimJSON(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\n' || s[i] == '\r' || s[i] == '\t') {
		i++
	}
	j := len(s)
	for j > i && (s[j-1] == ' ' || s[j-1] == '\n' || s[j-1] == '\r' || s[j-1] == '\t') {
		j--
	}
	return s[i:j]
}

func (c *collector) trim() {
	trimmed := make([]Block, 0, len(c.msg.Blocks))
	for _, b := range c.msg.Blocks {
		if b.Type == "" {
			continue
		}
		trimmed = append(trimmed, b)
	}
	c.msg.Blocks = trimmed
}
