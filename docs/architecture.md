# Argus architecture

This document explains how the Argus desktop app is built, how data moves through it, and where that data lives. It is meant to be the starting point for anyone working on the codebase. For deeper dives on specific areas, see [docs/architecture/chat-pipeline.md](architecture/chat-pipeline.md) and [docs/architecture/gateway/README.md](architecture/gateway/README.md).

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

Argus stores different kinds of data in different places on purpose: structured app state in SQLite, secrets in the OS keyring, large or user-facing files on disk, and transient runtime state in memory.

### SQLite database

**Location:** `os.UserConfigDir()/argus/argus.db` (`Storage/storage.go`, `DefaultPath`).

**Connection and durability settings:** `Storage.Open` creates the directory, opens the database, sets `SetMaxOpenConns(1)` so there is a single in-process writer connection, and enables:

- `PRAGMA journal_mode=WAL`
- `PRAGMA busy_timeout=5000`
- `PRAGMA foreign_keys=ON`

Schema changes are managed with `PRAGMA user_version` migrations in `Storage/migrations.go` (currently migrations 1 through 21). The app runs migrations on open.

### What the database stores

The schema grew over time, but the main groups are stable.

#### Provider, models, and agent configuration

- `provider_configs`: provider metadata such as `provider_id`, `secret_ref`, `key_last4`, `validated_at`, and `base_url_override`. Secret values are not stored here.
- `model_slots`: named slot assignments (`slot`, `model_ref`).
- `custom_models`: user-defined model entries and capabilities/pricing metadata.
- `provider_selected_models`: which models are selected/enabled per provider.
- `agent_profiles`: agent identity and settings (`id`, `avatar`, `agent_name`, `model_ref`, `updated_at`, later `handle`, `role`, `role_instructions`). Special profiles include the default agent and a hidden `__global_memory__` owner used by the memory system.

#### Chat history and model execution telemetry

- `messages`: the conversation log. Important fields include `id`, `agent_id`, `seq`, `role` (`user` or `assistant`), `blocks_json`, `text`, `status` (`complete`, `streaming`, `error`, `cancelled`, `interrupted`), `stop_reason`, `model_ref`, timestamps, `error_message`, `error_code`, and `kind`.
- `seq_counters`: per-agent sequence allocation (`agent_id`, `next_seq`) so message ordering stays consistent.
- `model_requests`: one row per model call/request attempt with provider/model/adapter info, context sizing, status, HTTP/provider identifiers, stop/error details, token usage, cost, and timing fields.

Message kinds used by the app include `steering`, `scheduled`, `delegated`, `tool_round`, and `file_write` (`Storage/messages.go`). Visible chat history, search, and pagination are built on top of these rows.

On startup/recovery, storage can mark stranded in-flight work as interrupted (`messages` stuck in `streaming`, `model_requests` stuck in `started`) without deleting user data.

#### Permissions and notices

- `permission_grants`: durable per-agent tool decisions (`agent_id`, `tool`, `mode` allow/deny).
- `permission_notices`: an audit/notice log for permission decisions and other notable events (for example MCP sign-in results), with fields like `agent_id`, `tool`, `command`, `decision`, `kind`, and `notice_text`.

#### Memory and feedback

- `user_memories`: legacy key/value memory table kept for migration/history.
- `memory_items`: structured memory records (`agent_id`, `key`, `value`, `scope`, `confidence`, `source_type`, revision/source metadata, timestamps). This is the main durable memory store.
- `memory_exposures`: which memories were shown to which model request, with position/score/token estimates.
- `message_reactions`: user reactions on messages (`emoji`, `signal`, timestamps).
- `memory_avoid_rules`: rules about things the agent should avoid.
- `learned_tool_lessons`: lessons learned from tool/MCP usage per agent/server/tool.

Earlier temporary agent memory storage was folded into `memory_items` as agent notes during migrations.

#### Scheduler

- `scheduled_tasks`: routines and reminders (`agent_id`, `kind`, `title`, `prompt`, schedule JSON, `next_run_at`, last run/status, paused flags, timestamps).
- `scheduled_runs`: execution history for tasks (`task_id`, `agent_id`, `message_id`, status started/success/error, error text, timing).

#### Artifacts

- `artifacts`: saved documents/decks (`agent_id`, `title`, `format`, `body`, `words`, `bytes`, `version`, timestamps, `meta`, `kind`, `file_path`).
- `artifact_versions`: previous versions of artifact bodies/titles for history.

Document content is stored in SQLite. For deck artifacts, a rendered `.pptx` file can also be written to disk and referenced by `file_path`.

#### Uploads

- `attachments`: metadata for uploaded files (`agent_id`, `filename`, `mime_type`, `size_bytes`, `data_path`, `extracted_text`, `created_at`). The raw bytes live on disk; the database keeps the path and extracted text.

#### MCP and multi-agent coordination

- `mcp_connections`: connected MCP accounts/servers (`server_id`, label, account/expected email, scopes, status, timestamps).
- `agent_connections`: which agent can use which MCP connection and at what access level (`read`, `draft`, `full`).
- `agent_messages`: queued/delivered/completed messages between agents/threads (delegation/inbox mechanics).
- `agent_run_metrics`: per-run metrics (rounds, tokens, tool calls, duration, status).
- `agent_skills`: per-agent enabled/disabled skills.

#### Account and app settings

- `account_entitlements`: cached entitlements snapshot (`payload`, `fetched_at`) used for offline access within a grace period.
- `app_settings`: lightweight key/value settings (for example tenant tracking and UI/app preferences).

### OS keyring

Secrets stay in the OS keyring, not in the SQLite database or frontend state.

- **Paired account key:** stored under the `argus` keyring service as `account/key` (`Keyring/keyring.go`, `Services/Account/service.go`). The frontend only sees pairing/account status and errors.
- **MCP OAuth/session secrets:** stored per server/connection (for example `mcp/<serverID>/session` or connection-specific refs) with serialized session/token material such as refresh/access tokens, expiry, scopes, and client/session metadata (`Services/MCP/tokens.go`).

When account-scoped data is purged, MCP session secrets for known connections are removed from the keyring as part of cleanup.

### Files on disk

Besides the SQLite database, Argus writes a few kinds of files under user config/cache directories:

- **Uploads:** `os.UserConfigDir()/argus/uploads/<attachment-id><ext>` (written with restrictive permissions). Uploads are validated for size/type, text is extracted for supported document types, and the file is deleted if the database insert fails.
- **Deck exports:** `os.UserConfigDir()/argus/decks/<id>.pptx` for generated slide decks.
- **Browser profile:** `os.UserConfigDir()/argus/browser` by default (overridable with `ARGUS_BROWSER_PROFILE_DIR`). This is an Argus-owned Chrome profile used for automation so logins/cookies/cache are isolated from the user’s normal browser.
- **Catalog cache:** `os.UserCacheDir()/argus`, used by the catalog store/background refresh.
- **Credential/config inputs:** MCP credential resolution can read files such as `config/<name>.json`, `.config/argus/<name>.json`, or `$HOME/.config/argus/<name>.json` depending on the connector/setup. The repo also includes `config/google.json` for development/configuration scenarios.

### In-memory state

Some state is intentionally ephemeral:

- `Core` holds runtime handles and listeners (overlay, DB handle, catalog store, notifier, interaction/session maps).
- Chat tracks active runs, cancellations, live tool sessions, and interactive request state.
- MCP keeps live connections/sessions and pending connection cards in memory, while durable tokens/connections live in keyring/SQLite.
- Browser keeps a long-lived Chrome/CDP session, open tabs/pages, takeover state, and idle timers in memory.
- The catalog and entitlements layers combine in-memory/runtime objects with on-disk or SQLite caches.

### Configuration and environment variables

- `frontend/.env` (build-time): `VITE_ANYSCAPE_GATEWAY_URL` sets the gateway origin the desktop app is built against. `VITE_ARGUS_API_URL` is a deprecated alias. In practice, model traffic follows the gateway the key paired against (persisted as a provider base URL override).
- `ARGUS_LOAD_DOTENV=1`: opt-in for development. When set, MCP credential loading may read `.env`-style files to pick up OAuth client IDs/secrets. This is off by default in packaged builds.
- `ARGUS_BROWSER_PROFILE_DIR`: overrides the base directory for the Argus browser profile.

## Account pairing, entitlements, and data isolation

### Pairing and verification

The account service (`Services/Account/service.go`) manages the desktop agent key. Keys are saved to and read from the OS keyring. When a key is saved or verified, Argus calls the gateway entitlements endpoint (`/api/v1/account/entitlements`) with the key, caches a successful entitlements snapshot in SQLite, and applies the provider base URL override for routed model traffic.

### Offline behavior

Entitlements responses are cached in `account_entitlements`. If the gateway cannot be reached, Argus can serve the cached snapshot within an offline grace period (7 days) and mark the account view as offline. Auth failures (invalid/revoked key, etc.) clear the cached entitlements instead of reusing them.

### Sign-out and account switching

Sign-out removes the key from the keyring, deletes the cached entitlements snapshot, and removes the router provider override.

When a different account is paired, Argus isolates tenant data. `Services/Account/tenant.go` tracks the current tenant in `app_settings` (`account/tenant_user_id`). If the new user differs from the previous one, `Storage.PurgeAccountTables()` wipes account-scoped local data, removes the router provider override, and records the new tenant.

The purge removes account-scoped rows across chat, memories, reactions, artifacts, attachments metadata, requests, scheduler data, permissions/notices, model/profile selections, MCP connections/bindings, skills/lessons, and agent profiles/counters. Before database deletion it best-effort deletes known attachment files from disk and removes MCP session secrets from the keyring. After the wipe it reseeds required profiles: the default agent and `__global_memory__`.

Some things are not treated as tenant chat content in that purge (for example the browser profile directory and general cache directories may remain), but account-scoped secrets and conversation data are cleared so one account does not inherit another account’s local workspace.

## Backend services and responsibilities

The backend is organized by domain under `Services/`. The most important ones are:

- **Shared (`Services/Shared`)**: core bootstrap, event names, shared DTO/view types, typed errors, run/interaction context helpers.
- **Account (`Services/Account`)**: key storage/verification, entitlements fetch/cache/offline handling, tenant isolation.
- **Provider, Models, Catalog**: provider overlay/configuration, model selection/slots/profiles, catalog refresh and lookup. Details are in `docs/architecture/gateway/`.
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

Chat is the central workflow in Argus. `docs/architecture/chat-pipeline.md` describes the message pipeline in detail; the short version is below.

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

- **Permissions**: `permission:requested` / `permission:resolved` events gate sensitive actions. Unattended runs refuse interactive approvals and record a notice instead of hanging.
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

- [Chat pipeline](architecture/chat-pipeline.md): stage-by-stage behavior for chat turns, tool rounds, persistence, and presentation.
- [Gateway overview](architecture/gateway/README.md): provider, models, and catalog service architecture.
- [Gateway providers](architecture/gateway/providers.md), [models](architecture/gateway/models.md), [catalog](architecture/gateway/catalog.md): detailed gateway subsystem docs.

If you change storage schema, service contracts, or event payloads, update the relevant docs alongside the code so this overview stays trustworthy.
