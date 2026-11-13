package Chat

import (
	"encoding/json"

	"github.com/anyscapelabs/argus/Protocol"
)

// redactPersistedToolInputs keeps executed payloads out of durable history.
// The immediate next model round still receives the original call; later turns
// can read files/artifacts deliberately instead of replaying entire bodies.
func redactPersistedToolInputs(message Protocol.Message) Protocol.Message {
	out := Protocol.Message{Role: message.Role, Blocks: make([]Protocol.Block, len(message.Blocks))}
	for i, block := range message.Blocks {
		out.Blocks[i] = block.Clone()
		call := out.Blocks[i].ToolCall
		if call == nil || !call.Valid {
			continue
		}
		if call.Name == liveInvokeToolDef.Name {
			var outer map[string]any
			if json.Unmarshal([]byte(call.Input), &outer) == nil {
				name, _ := outer["name"].(string)
				argsText, _ := outer["arguments_json"].(string)
				outer["arguments_json"] = redactArguments(name, argsText)
				if raw, err := json.Marshal(outer); err == nil {
					call.Input = string(raw)
				}
			}
			continue
		}
		call.Input = redactArguments(call.Name, call.Input)
	}
	return out
}

func redactArguments(name, input string) string {
	field := ""
	switch name {
	case "write_file":
		field = "content"
	case "edit_file":
		var args map[string]any
		if json.Unmarshal([]byte(input), &args) != nil {
			return input
		}
		args["old_text"] = "[redacted after successful tool execution]"
		args["new_text"] = "[redacted after successful tool execution]"
		raw, err := json.Marshal(args)
		if err != nil {
			return input
		}
		return string(raw)
	case "write_artifact":
		field = "body"
	case "create_slides":
		field = "slides"
	}
	if field == "" {
		return input
	}
	var args map[string]any
	if json.Unmarshal([]byte(input), &args) != nil {
		return input
	}
	if _, exists := args[field]; exists {
		args[field] = "[redacted after successful tool execution]"
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return input
	}
	return string(raw)
}
