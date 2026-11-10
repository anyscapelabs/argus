package Shared

// LiveConfig is everything the frontend needs to open a live voice call: a
// ready-to-open WebSocket URL (the key is already in its query, since the
// browser cannot read the OS keyring). The model is deliberately not part of it
// — the router resolves its configured default and injects it into the setup
// frame, so the desktop never has to know a model id.
type LiveConfig struct {
	WsURL string `json:"ws_url"`
}

type LiveConfigResult struct {
	Config *LiveConfig  `json:"config,omitempty"`
	Error  *ActionError `json:"error,omitempty"`
}

// ConversationReadResult is the text a voice session's conversation_read tool
// call should see. Text is the tool_result body the model reads.
type ConversationReadResult struct {
	Text  string       `json:"text"`
	Error *ActionError `json:"error,omitempty"`
}
