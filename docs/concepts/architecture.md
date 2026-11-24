# Argus architecture

This document explains how the Argus desktop app is built, how data moves through it, and where that data lives. It is meant to be the starting point for anyone working on the codebase. For deeper dives on specific areas, see [the chat pipeline](chat-pipeline.md) and [the gateway architecture](gateway/README.md).

## What Argus is

Argus is a desktop AI agent app. The backend is written in Go with [Wails v3](https://v3.wails.io/), and the frontend is a React 18 + TypeScript app bundled into the desktop shell. The app talks to the Anyscape Gateway for model access, keeps local state in SQLite, and stores secrets in the operating system keyring.

At a high level:

- `frontend/` renders the UI and calls backend methods through generated Wails bindings.
- `Services/` contains the backend domains: chat, account, permissions, MCP, browser automation, scheduler, uploads, artifacts, and others.
- `Storage/` owns the local SQLite database, schema migrations, and data access helpers.
- `Provider/`, `Catalog/`, `Router/`, and `Protocol/` handle the embedded provider overlay, model catalog, request routing, and provider protocol adapters.
- `Keyring/` wraps secret storage.

## How the app starts and runs

### Backend bootstrap

`main.go` is the entry point. It embeds the frontend build (`frontend/dist`) and the app icon, forces an X11 backend on Linux before window creation, and then builds the app in a fixed order:

1. Create the shared core with `Shared.NewCore()`.
2. Construct domain services (`Account`, `Provider`, `Models`, `Catalog`, `Permission`, `MCP`, `MCPConnect`, `Artifacts`, `Uploads`, `Browser`, `Skills`, `Chat`, `Scheduler`, notifications).
3. Wire cross-service callbacks that avoid import cycles. For example, when an MCP connection settles, `MCPConnect.OnSettled` steers the chat service so the agent can react to the new connection.
4. Register Wails services for the frontend. Browser automation is started and owned by chat, so it is not exposed as a separate UI-bound service.
5. Create the main frameless window and bridge backend events into the Wails runtime.
6. Start the scheduler and run the app.

`Services/Shared/shared.go` defines the shared `Core`. On first bootstrap it loads the embedded provider overlay (`Provider.Load()`), opens the SQLite database at `Storage.DefaultPath()`, creates the catalog store (using `os.UserCacheDir()/argus` as cache space), and starts background catalog refresh. `Core.EmitEvent` fans events out to the Wails frontend and to in-process listeners, which lets services react to each other without direct coupling.

### Background behavior and window lifecycle

`background_lifecycle.go` keeps the app alive when the window is hidden. Closing the window hides it instead of quitting, a tray icon can restore it, and the app tracks active work from `chat:run_started` and `chat:run_finished` events to show whether it is working. This matters because chat runs, scheduled tasks, and browser sessions can continue after the UI window is closed.

### Frontend structure

`frontend/src/App.tsx` owns app-level navigation and state. Instead of a router library, it switches between pages such as sign-in, onboarding, home, chat, settings, integrations, and integration detail. It also mounts global overlays (artifact viewer, new agent flow), applies theme/accent preferences, and listens for backend events to invalidate React Query caches when providers, selections, slots, or the catalog change.

The frontend follows a domain pattern under `frontend/src/domains/*` (`chat`, `account`, `models`, `mcp`, `permission`, `uploads`, `artifacts`, `browser`, `askUser`, `live`, `scheduler`, `usage`, and others). Each domain usually has:

- `api.ts` to call generated bindings and normalize errors.
- `hooks.ts` to expose React Query queries/mutations and event-driven state.

Generated bindings live in `frontend/bindings/`. If a Go service method signature changes, bindings need to be regenerated (`wails3 generate bindings`).

## Data storage

Argus stores different kinds of data in different places on purpose:

- **SQLite** (`os.UserConfigDir()/argus/argus.db`) holds structured app state: messages, model request telemetry, memories, permissions, schedules, artifacts, MCP connections, and cached entitlements. The database opens in WAL mode with a single writer connection (`SetMaxOpenConns(1)`), and schema changes are versioned with `PRAGMA user_version` migrations.
- **OS keyring** holds secrets: the paired account key and MCP OAuth/session tokens. The database and the frontend never see secret values.
- **Disk** holds uploaded files, generated deck exports, the browser automation profile, and the catalog cache.
- **Memory** holds runtime-only state: active runs, live MCP sessions, open browser tabs, and pending interaction cards.

The full table-by-table schema, keyring refs, disk paths, and cleanup flows are in the [storage reference](../reference/storage.md).

### Account pairing, entitlements, and data isolation

The account service (`Services/Account/service.go`) manages the desktop agent key. Keys are saved to and read from the OS keyring. When a key is saved or verified, Argus calls the gateway entitlements endpoint (`/api/v1/account/entitlements`), caches a successful snapshot in SQLite, and applies the provider base URL override for routed model traffic. Cached entitlements can be served offline within a grace period (7 days); auth failures clear the cache instead of reusing it.

Sign-out removes the key, the cached snapshot, and the router override. When a different account is paired, `Services/Account/tenant.go` triggers tenant isolation: `Storage.PurgeAccountTables()` wipes account-scoped local data (chats, memories, artifacts, attachments, schedules, permissions, MCP bindings, skills, profiles), best-effort deletes known attachment files and MCP keyring secrets, and reseeds the required default profiles. This prevents one account's workspace from leaking into another on the same machine. Details are in [security](security.md) and the [storage reference](../reference/storage.md).

## Backend services and responsibilities

The backend is organized by domain under `Services/`. The most important ones are:

- **Shared (`Services/Shared`)**: core bootstrap, event names, shared DTO/view types, typed errors, run/interaction context helpers.
- **Account (`Services/Account`)**: key storage/verification, entitlements fetch/cache/offline handling, tenant isolation.
- **Provider, Models, Catalog**: provider overlay/configuration, model selection/slots/profiles, catalog refresh and lookup. Details are in the [gateway architecture](gateway/README.md).
- **Chat (`Services/Chat`)**: message send/read APIs, prompt building, history replay/repair, agent loop, tool execution, streaming, reactions, ask-user, voice/live flows, takeover steering, delegation helpers.
- **Permission (`Services/Permission`)**: policy storage and runtime approval gates for sensitive tools (terminal, file access, browser actions, skill install, browser use), including unattended-run refusal behavior.
- **MCP (`Services/MCP`) and MCPConnect (`Services/MCPConnect`)**: MCP server/session management, OAuth flows, token persistence, per-agent connection bindings/access tiers, connect/reauth UI cards, and tool exposure.
- **Uploads (`Services/Uploads`)**: file validation, disk storage, text extraction, attachment metadata/views.
- **Artifacts (`Services/Artifacts`)**: document and deck persistence/versioning, PPTX generation for decks, export to user-chosen formats/paths.
- **Browser (`Services/Browser`)**: Chrome/CDP automation session, navigation/reading/snapshots/actions, tab/state tracking, idle shutdown, and takeover-aware control.
- **Scheduler (`Services/Scheduler`)**: durable routines/reminders backed by `scheduled_tasks`/`scheduled_runs`, due scanning, coalescing/backoff/offline handling, and unattended wake execution through chat.
- **Skills (`Services/Skills`)**: embedded skill catalog/content and per-agent enablement for progressive skill loading.
- **Memory, Location, Search, Slides, Terminal**: supporting domains used by chat tools and prompting.

## Chat and agent execution

Chat is the central workflow in Argus. [the chat pipeline document](chat-pipeline.md) describes the message pipeline in detail; the short version is below.

### Sending a message

When the frontend sends a message (`Services/Chat/send.go`), the backend:

1. Validates readiness, agent/profile/model selection, provider configuration, and plan/model allowlist before persisting the turn.
2. Reserves sequence numbers and inserts the user message and a streaming assistant placeholder into `messages`.
3. Builds context from recent history, attachments, memories, skills, connected integrations, and tool definitions.
4. Creates a `model_requests` record and starts a cancellable run.
5. Emits chat/run events so the UI can update immediately.

Scheduled wakes and agent steering follow related paths but use different message kinds and run contexts (for example unattended scheduled runs).

### History, prompt, and replay

History is read from SQLite with pagination and visibility rules. Before a model call, chat rebuilds a provider-safe replay window: it selects recent turns within budget, repairs tool-call/result pairing, drops invalid or orphaned pieces as needed, and can compact context during long runs. The system prompt is assembled in `Services/Chat/prompt.go` from the base persona/behavior rules plus run-specific context (memories, skills, integrations, location, scheduled-fire instructions, and tool guidance).

### The agent loop

`runTurn` in `Services/Chat/agent_loop.go` drives the model/tool loop. It streams model output, collects tool calls, executes tools, persists tool rounds, handles retries and compaction, and finalizes the assistant message with status/usage/cost. Runs are bounded (for example by a maximum tool-round backstop) and emit progress events such as `chat:typing`, `chat:phase`, `agent:activity`, `chat:message`, and `chat:run_finished`.

Tools include built-in capabilities (file/terminal operations, search, scheduling, memory, artifacts, browser, ask-user, conversation read, etc.) and MCP tools exposed through the chat/MCP gateway. Tool access is checked against permissions and MCP access tiers before side effects occur.

### Interactive flows inside chat

Some actions pause for user input or confirmation:

- **Permissions**: `permission:requested` / `permission:resolved` events gate sensitive actions. Unattended runs hold the action and record an actionable notice shown in the chat instead of hanging.
- **Ask user**: the `ask_user` tool emits a request card and waits for a response (or is refused in unattended contexts).
- **MCP connect**: connection/auth cards are managed by `MCPConnect`, with settlement notices and optional steering back into chat.
- **Browser takeover**: when the user needs to take control (or a sensitive browser step requires it), takeover state is tracked and surfaced to the UI.

## MCP and integrations

MCP support is split between durable connection state, secret storage, runtime sessions, and UI orchestration:

- `mcp_connections` and `agent_connections` in SQLite define which external accounts/servers exist and which agents can use them.
- OAuth/client/session secrets live in the OS keyring.
- `Services/MCP` manages live sessions, tool discovery, and tool calls (local stdio servers and remote OAuth-backed servers).
- `Services/MCPConnect` handles the user-facing connect/bind/reauth flow, keeps one live card per agent where appropriate, verifies expected account identity when required, and emits `mcp:connect:*` and `integrations:changed` events.

Chat exposes MCP capabilities progressively so the model does not need every tool schema attached on every turn. Connected integrations and access level determine what an agent can see and do.

## Browser automation

`Services/Browser` runs an Argus-owned Chrome instance over CDP. It manages navigation, page snapshots/reads, screenshots/captures, and actions, while tracking open tabs and takeover/blocked states. The browser profile is stored on disk under the Argus config directory so automation state is persistent but separate from the user’s personal browser profile. Sensitive or irreversible browser actions are expected to flow through permission and takeover checks in the chat/tool path.

## Scheduler and background work

The scheduler is database-driven. `scheduled_tasks` stores what should run and when (`next_run_at`), and `scheduled_runs` records each execution attempt. The scheduler loop wakes on a timer and on relevant events, sweeps due or missed work, applies coalescing/backoff rules, and fires tasks by sending an unattended scheduled wake into chat. Because tasks and runs are persisted, missed work can be recovered after restarts or offline periods.

## Frontend and backend communication

The frontend talks to Go services through generated Wails bindings for request/response calls, and subscribes to runtime events for streaming and state changes. Common event families include catalog/provider/model updates, chat streaming and run lifecycle, permissions, ask-user, MCP connect/integration changes, browser takeover, artifacts, and schedule updates. React Query is used for caching and invalidation on the frontend; backend services emit events when durable or user-visible state changes.

## Key architectural decisions

These decisions show up repeatedly in the codebase:

- **Single routed model provider:** model traffic goes through the embedded `argus-router` overlay toward the paired Anyscape Gateway, with entitlements and allowlists enforced server-side and treated conservatively on the client.
- **Local-first state with explicit boundaries:** SQLite holds structured state, the keyring holds secrets, and the filesystem holds uploaded/generated files. The frontend never receives secret material.
- **Single-writer SQLite:** `SetMaxOpenConns(1)` and WAL mode keep writes predictable in a desktop app with many subsystems sharing one database.
- **Durable runs and recoverability:** chat messages/requests, scheduled tasks/runs, and connection metadata are persisted so work can be resumed, audited, or recovered after interruptions.
- **Event-driven UI:** long-running and interactive flows (streaming, approvals, connect cards, takeover) are communicated through events rather than blocking the UI on synchronous calls.
- **Account isolation:** pairing a different account wipes account-scoped local data to prevent cross-tenant leakage on the same device.
- **Safety gates around side effects:** permissions, unattended-run refusals, MCP access tiers, and browser takeover checks limit what automated actions can do without user awareness or approval.

## Build, test, and development notes

Common commands (see `README.md` and `Taskfile.yml`):

```sh
# frontend dependencies
cd frontend && npm install

# run with hot reload
wails3 dev -config ./build/config.yml -port 9245

# production build
wails3 build

# tests
go test $(go list ./... | grep -v build/ios)
cd frontend && npm test
```

`frontend/package.json` defines the Vite/Vitest scripts, and `build/config.yml` contains Wails build/dev configuration. `go.mod` tracks the Go toolchain and major backend dependencies (Wails v3, SQLite driver, keyring, MCP SDK, websocket/OAuth libraries, document parsing, etc.).

## Related documentation

- [Chat pipeline](chat-pipeline.md): stage-by-stage behavior for chat turns, tool rounds, persistence, and presentation.
- [Gateway overview](gateway/README.md): provider, models, and catalog service architecture.
- [Security](security.md): trust boundaries, secrets, and isolation behavior.
- [Tools reference](../reference/tools.md), [events reference](../reference/events.md), [configuration reference](../reference/configuration.md), [storage reference](../reference/storage.md).

If you change storage schema, service contracts, or event payloads, update the relevant docs alongside the code so this overview stays trustworthy.
