package Shared

// LiveConfig is everything the frontend needs to open a live voice call: a
// ready-to-open WebSocket URL (the key is already in its query, since the
// browser cannot read the OS keyring). The model is deliberately not part of it
// — the router resolves its configured default and injects it into the setup
// frame, so the desktop never has to know a model id.
type LiveConfig struct {
	WsURL             string                    `json:"ws_url"`
	SessionID         string                    `json:"session_id"`
	SystemInstruction string                    `json:"system_instruction"`
	Tools             []LiveFunctionDeclaration `json:"tools"`
}

// LiveFunctionDeclaration is one of the deliberately small gateway functions
// sent at Live setup. The full tool catalog stays in the backend and is reached
// lazily through search_tools and invoke_tool.
type LiveFunctionDeclaration struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Behavior    string         `json:"behavior"`
	Parameters  map[string]any `json:"parameters"`
}

type LiveConfigResult struct {
	Config *LiveConfig  `json:"config,omitempty"`
	Error  *ActionError `json:"error,omitempty"`
}

// LiveToolResult is the backend's answer to one Live function call. Tool-level
// failures are returned to the model through Output/IsError; Error is reserved
// for application failures the frontend should surface.
type LiveToolResult struct {
	Output  string       `json:"output"`
	IsError bool         `json:"is_error"`
	Error   *ActionError `json:"error,omitempty"`
}

// VoiceTurn is one spoken side of a voice call: what the user said or what
// the assistant said, transcribed by the live session.
type VoiceTurn struct {
	Role string `json:"role"` // "user" or "model"
	Text string `json:"text"`
}

// VoiceCallResult reports whether a finished voice call was persisted.
type VoiceCallResult struct {
	Ok    bool         `json:"ok"`
	Error *ActionError `json:"error,omitempty"`
}
