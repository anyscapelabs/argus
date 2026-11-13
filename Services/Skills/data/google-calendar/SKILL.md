---
name: google-calendar
description: Read, schedule, update, and delete Google Calendar events.
---

# Google Calendar Workflow

All Google Calendar tools are prefixed `google_calendar__`.

## Standard Chains
1. **Check Clock & Timezone First**: Always call `get_time_and_location` before resolving relative dates ("tomorrow", "next Friday", "this afternoon").
2. **List / Check Availability**: Call `google_calendar__list_events` with `calendarId: "primary"` and ISO-8601 `timeMin` / `timeMax` bounds (include timezone offset).
3. **Create Event**: Call `google_calendar__create_event` with `calendarId: "primary"`, `summary`, `start`, and `end` in RFC3339 format.
4. **Update / Cancel Event**: First list events in the target time window to obtain the exact `eventId`, then call `google_calendar__update_event` or `google_calendar__delete_event`.

## Gotchas
- Always include the user's local timezone offset in RFC3339 timestamps (e.g. `2026-11-13T15:00:00+02:00`), never bare dates for timed meetings.
