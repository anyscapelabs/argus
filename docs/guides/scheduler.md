# Scheduler and background tasks

How durable routines and reminders work in Argus. Implementation lives in
`Services/Scheduler` with storage in `Storage/scheduled_tasks.go`.

## The model

Scheduling is database-driven, not in-process timers:

- `scheduled_tasks` stores what should run and when: kind (reminder vs
  routine), title, prompt, schedule JSON, `next_run_at`, last run/status, and
  paused flags.
- `scheduled_runs` records each execution attempt: task, agent, resulting
  message, status (started/success/error), error text, and timing.

Because both sides are persisted, missed work can be recovered after restarts
or offline periods.

## How a task fires

1. The agent creates work with `schedule_reminder` (one-time) or
   `create_routine` (recurring); the user can also manage tasks in the routines
   panel. State changes emit `schedule:updated`.
2. The scheduler loop wakes on a timer and on relevant events, sweeps for due
   or missed tasks, and applies coalescing and backoff rules.
3. A due task fires as an **unattended scheduled wake** sent into chat. The
   resulting turn is a `scheduled` message kind with its own run context.
4. The run is recorded in `scheduled_runs`; outcomes emit events/notifications.

## Unattended execution constraints

Scheduled wakes run without a user watching, so interactive flows behave
differently:

- Permission requests are **refused**, not awaited: the action is denied and a
  notice is recorded in `permission_notices` (see
  [security](../concepts/security.md)).
- The `ask_user` tool is likewise refused.

This is deliberate: an unattended run must never hang waiting for input nobody
will give.

## Where to change what

- Schedule semantics (cron/coalescing/backoff) → `Services/Scheduler`.
- New task field → `Storage/migrations.go` + `Storage/scheduled_tasks.go`, and
  consider purge coverage for account-scoped data.
- UI → `frontend/src/domains/scheduler` (the routines panel redraws from
  `schedule:updated`, never polls).
