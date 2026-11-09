package Chat

import (
	"encoding/json"
	"github.com/anyscapelabs/argus/Protocol"
)

// writeArtifactToolDef is the document tool. The rules the validator enforces
// live in the system prompt rather than here: a tool description is read once,
// while the list of rejected constructs is the thing the model needs next to the
// text it is composing.
var writeArtifactToolDef = Protocol.ToolDef{
	Name: "write_artifact",
	Description: "Write one document into the app's Artifacts panel. Markdown only; the user exports it " +
		"later as .md, .html, .pdf or .docx. Free, no approval, and it never touches the user's disk. " +
		"Use it for anything worth keeping — a report, brief, plan, letter, summary, comparison — " +
		"instead of pasting long markdown into the chat.",
	Schema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"title": {
				"type": "string",
				"description": "Short label the user sees in the panel, e.g. \"November sales report\""
			},
			"body": {
				"type": "string",
				"description": "The document in Markdown. Optional front matter first (title, author, date, language, paper, orientation). On a rewrite this is the whole new document, not a diff."
			},
			"artifact_id": {
				"type": "string",
				"description": "Id returned by an earlier write_artifact call. Omit to start a new document."
			},
			"append": {
				"type": "boolean",
				"description": "true adds body to the end of that document instead of replacing it. Use it to write a long document in parts."
			}
		},
		"required": ["body"]
	}`),
}
