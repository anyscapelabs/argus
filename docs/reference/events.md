# Events reference

The backend communicates with the frontend (and lets services react to each
other) through named events. `Core.EmitEvent` fans each event out to the Wails
frontend and to in-process listeners (`Services/Shared/shared.go`). The
authoritative list of names lives in `Services/Shared/events.go`; this page
documents what each one means and when it fires. If you add or rename an event,
update both places in the same change.

## Catalog, providers, and models

| Event | Fired when |
|---|---|
| `catalog:updated` | The model catalog store refreshes (background or manual). |
| `providers:changed` | Provider configuration/connect state changes. |
| `slots:changed` | A model slot assignment (`main`, `cheap`, `fallback`) changes. |
| `selections:changed` | Selected/enabled models per provider change. |

The frontend listens for these to invalidate React Query caches for provider,
selection, slot, and catalog data (see `frontend/src/App.tsx`).

## Model streaming

| Event | Fired when |
|---|---|
| `model:stream` | A provider streams model output tokens/blocks during a run. |
| `model:stream_cancelled` | An in-flight stream is cancelled (user or system). |

## Chat and run lifecycle

| Event | Fired when |
|---|---|
| `chat:run_started` | An agent run begins (also used to track active work for the tray/lifecycle indicator). |
| `chat:run_finished` | An agent run completes, errors, or is cancelled. |
| `chat:typing` | The assistant is producing output for the current turn. |
| `chat:phase` | The run moves between phases (for example thinking vs acting). |
| `chat:message` | A message row is created or updated (user, assistant, tool round). |
| `agent:activity` | Fine-grained activity during tool execution, for the activity display. |

## Permissions

| Event | Fired when |
|---|---|
| `permission:requested` | A sensitive tool action needs an approval decision. |
| `permission:resolved` | The user (or policy) resolves the request. |

Unattended runs never emit a blocking request; they refuse and record a notice
instead. See [security](../concepts/security.md).

## Ask user

| Event | Fired when |
|---|---|
| `ask_user:requested` | The agent calls `ask_user` and waits for input. |
| `ask_user:resolved` | The user responds (or the request is refused in unattended contexts). |

## MCP and integrations

| Event | Fired when |
|---|---|
| `mcp:connect:requested` | A connect/bind/reauth card is shown for an MCP connection. |
| `mcp:connect:resolved` | The user completes or dismisses the card. |
| `mcp:connect:status` | The connection card's status changes during the flow. |
| `integrations:changed` | The set of usable integrations changes (connect, disconnect, reauth). |

`MCPConnect.OnSettled` steers the chat service when a connection settles, so the
agent can react to newly available tools.

## Browser takeover

| Event | Fired when |
|---|---|
| `browser:takeover:requested` | The agent (or a captcha detection) offers the user the browser window. |
| `browser:takeover:status` | Later steps of the takeover card's life: user takes over, finishes, or declines. |

## Artifacts

| Event | Fired when |
|---|---|
| `artifact:updated` | The agent writes or extends a document, or one is deleted; the artifacts panel refetches on this event instead of polling. |

## Scheduler

| Event | Fired when |
|---|---|
| `schedule:updated` | `scheduled_tasks` changes — the agent creates or cancels one, or the engine advances, retires, or pauses it — so the routines panel redraws from the database. |

## App updates

| Event | Fired when |
|---|---|
| `updates:available` | The background update checker finds a build newer than the running one. The payload is the updater `Status` (`Services/Updater`): running and latest versions, release notes, and the release page. The "This App" page refetches on it, so the Update button appears without the user pressing "Check for update". |

## Conventions

- Event payloads are DTOs from `Services/Shared/views_*.go`; changing a payload
  shape means regenerating bindings (`wails3 generate bindings`).
- Events are the UI's update channel: panels redraw from events (and refetch)
  rather than polling, so emit an event whenever durable or user-visible state
  changes.
- The background lifecycle tracker watches `chat:run_started` /
  `chat:run_finished` to decide whether the app is working while hidden
  (`background_lifecycle.go`).
