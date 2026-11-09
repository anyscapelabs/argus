package Chat

import (
	"encoding/json"
	"github.com/anyscapelabs/argus/Protocol"
)

var scheduleReminderToolDef = Protocol.ToolDef{
	Name: "schedule_reminder",
	Description: "Create a one-time reminder the app fires on its own schedule, even if this chat is closed. " +
		"Use for any single future request: \"remind me at 5\", \"tomorrow at 9 do X\", \"on Friday check Y\". " +
		"Free, no approval. Call get_time_and_location first and compute an absolute local time. " +
		"The prompt must be self-contained: it runs later, unattended, with nobody to ask.",
	Schema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"title": {
				"type": "string",
				"description": "Short label for the reminder, e.g. \"standup note\""
			},
			"prompt": {
				"type": "string",
				"description": "The instruction to run at that time, self-contained, e.g. \"Remind the user their dentist appointment is in 30 minutes\""
			},
			"at": {
				"type": "string",
				"description": "Local fire time as RFC3339 (2026-11-03T09:00:00Z07:00) or 2006-01-02T15:04. Must be in the future."
			}
		},
		"required": ["title", "prompt", "at"]
	}`),
}

var createRoutineToolDef = Protocol.ToolDef{
	Name: "create_routine",
	Description: "Create a recurring routine the app runs on its own schedule, even if this chat is closed. " +
		"Use for anything repeating: \"every morning\", \"weekdays at 8:30\", \"every 2 hours\", \"mondays and Thursdays\". " +
		"Free, no approval. Times are local wall clock. The prompt runs unattended: it must stand alone, " +
		"and if it needs the terminal the user must have granted Allow Always beforehand or the command is refused.",
	Schema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"title": {
				"type": "string",
				"description": "Short label for the routine, e.g. \"morning briefing\""
			},
			"prompt": {
				"type": "string",
				"description": "The instruction to run each time, self-contained"
			},
			"freq": {
				"type": "string",
				"enum": ["daily", "weekdays", "weekly", "monthly", "interval"],
				"description": "daily = every day at time; weekdays = Mon-Fri at time; weekly = chosen weekdays at time; monthly = one day-of-month at time; interval = every N minutes"
			},
			"time": {
				"type": "string",
				"description": "Local fire time as HH:MM for daily/weekdays/weekly/monthly. Omit for interval."
			},
			"weekdays": {
				"type": "array",
				"items": {"type": "integer"},
				"description": "For weekly only: days as 0=Sunday … 6=Saturday, e.g. [1,5] for Mon and Fri"
			},
			"day": {
				"type": "integer",
				"description": "For monthly only: day of month 1-31. Omit to use today's day of month."
			},
			"every_minutes": {
				"type": "integer",
				"description": "For interval only: repeat every N minutes (>= 1)"
			}
		},
		"required": ["title", "prompt", "freq"]
	}`),
}

var listScheduledToolDef = Protocol.ToolDef{
	Name:        "list_scheduled",
	Description: "List every reminder and routine the app has scheduled, with its id, next fire time and last result. Free, no approval.",
	Schema:      json.RawMessage(`{"type": "object", "properties": {}}`),
}

var cancelScheduledToolDef = Protocol.ToolDef{
	Name:        "cancel_scheduled",
	Description: "Delete a scheduled reminder or routine by its id (see list_scheduled). Free, no approval. The user saying \"cancel my morning briefing\" is enough to call it.",
	Schema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"task_id": {
				"type": "string",
				"description": "The id shown by list_scheduled or the creation confirmation"
			}
		},
		"required": ["task_id"]
	}`),
}
