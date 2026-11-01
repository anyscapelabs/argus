package Chat

// mcpAuthURL returns a browser URL the frontend should open as part of the
// connection flow for the given integration. Returns an empty string when no
// separate browser step is needed (e.g. the MCP server manages its own OAuth
// flow internally when it starts up).
//
// Populate this only when the integration requires the user to visit a
// fully-formed URL (with client_id, scope, redirect_uri, etc.) before the MCP
// server can be started. Raw OAuth endpoint base URLs without those parameters
// will be rejected by the provider (400 invalid_request).
func mcpAuthURL(serverID string) string {
	// All currently supported integrations (Gmail, Google Calendar, Todoist)
	// use MCP servers that handle their own OAuth browser pop-up at startup.
	// No separate URL is needed here.
	_ = serverID
	return ""
}
