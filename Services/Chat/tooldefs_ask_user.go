package Chat

import (
	"encoding/json"

	"github.com/anyscapelabs/argus/Protocol"
)

var askUserToolDef = Protocol.ToolDef{
	Name: "ask_user",
	Description: "Ask the user one concise question only when a missing fact or preference blocks progress. " +
		"Offer useful single-select choices; the user can also type a custom answer. The call waits and returns that answer. " +
		"Do not use this for permissions, browser takeover, or work an available tool can perform.",
	Schema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"question": {"type": "string", "description": "One concise question"},
			"choices": {
				"type": "array",
				"maxItems": 6,
				"items": {"type": "string"},
				"description": "One to six short, distinct suggested answers"
			}
		},
		"required": ["question", "choices"]
	}`),
}
