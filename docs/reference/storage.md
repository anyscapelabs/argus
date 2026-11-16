# Data and storage

This document describes where Argus stores data, what each storage layer is for, and how data is cleaned up on sign-out or account change.

## Storage layers

Argus uses four storage layers:

| Layer | Purpose | Examples |
|---|---|---|
| SQLite | Structured, queryable app state | Messages, artifacts, schedules, permissions, profiles, MCP bindings, cached entitlements |
| OS keyring | Secrets | Paired account key, MCP OAuth/session tokens |
| Files on disk | Large binaries, generated files, caches, profiles | Upload files, PPTX decks, browser profile, catalog cache |
| In-memory state | Runtime-only state | Active runs, live MCP sessions, open browser tabs, pending UI interaction cards |

## SQLite database

### Location and configuration

- **Path:** `os.UserConfigDir()/argus/argus.db` (`Storage/storage.go`)
- **Open behavior:** creates the parent directory, opens the database, sets a single writer connection (`SetMaxOpenConns(1)`), runs migrations, and enables pragmas:
  - `journal_mode=WAL`
  - `busy_timeout=5000`
  - `foreign_keys=ON`
- **Migrations:** versioned with `PRAGMA user_version` in `Storage/migrations.go` (migrations 1–21 at the time of writing).

### Schema overview

The tables below are grouped by purpose. For exact columns and migration details, see `Storage/migrations.go` and the corresponding helpers in `Storage/*.go`.

#### Provider, models, and agent configuration

- **`provider_configs`**: provider metadata (`provider_id`, `secret_ref`, `key_last4`, `validated_at`, `base_url_override`). No plaintext secrets.
- **`model_slots`**: named slot assignments (`main`, `cheap`, `fallback`, etc. as used by the app).
- **`custom_models`**: custom/user-defined model definitions and capability/pricing fields.
- **`provider_selected_models`**: selected/enabled models per provider.
- **`agent_profiles`**: agent identity and preferences (`id`, `avatar`, `agent_name`, `model_ref`, `handle`, `role`, `role_instructions`, timestamps). Includes default/system profiles such as `default` and `__global_memory__`.

#### Chat and model telemetry

- **`messages`**: persisted conversation turns.
  - Key fields: `id`, `agent_id`, `seq`, `role` (`user`/`assistant`), `blocks_json`, `text`, `status`, `stop_reason`, `model_ref`, timestamps, `error_message`, `error_code`, `kind`.
  - Status values include `complete`, `streaming`, `error`, `cancelled`, `interrupted`.
  - Message kinds include `steering`, `scheduled`, `delegated`, `tool_round`, and `file_write` (`Storage/messages.go`).
- **`seq_counters`**: per-agent next sequence number for ordered message insertion.
- **`model_requests`**: per-request telemetry and audit data (provider/model/adapter, attempt, context size, status, stop/error info, HTTP/provider IDs, token usage, cost, timings, usage source).

On startup/recovery, interrupted in-flight records can be marked as interrupted (for example streaming messages and started model requests) so the UI/state remains consistent.

#### Artifacts

- **`artifacts`**: saved documents and decks (`agent_id`, `title`, `format`, `body`, `kind`, `meta`, `words`, `bytes`, `version`, `file_path`, timestamps).
- **`artifact_versions`**: historical versions of artifact content.

Artifact bodies are stored in SQLite. Deck artifacts may also have a generated `.pptx` file on disk referenced by `file_path`. Version retention/pruning and size limits are enforced in artifact storage/service logic.

#### Uploads

- **`attachments`**: uploaded file metadata (`agent_id`, `filename`, `mime_type`, `size_bytes`, `data_path`, `extracted_text`, `created_at`).
  - File bytes are stored on disk; SQLite keeps metadata, path, and extracted text for supported document types.

#### Memory and feedback

- **`memory_items`**: structured durable memory (`agent_id`, `key`, `value`, `scope`, `confidence`, source/revision fields, timestamps).
- **`memory_exposures`**: which memories were exposed to which model requests (position/score/token estimates).
- **`message_reactions`**: emoji/signal feedback on messages.
- **`memory_avoid_rules`**: agent-specific avoid rules.
- **`learned_tool_lessons`**: lessons from tool/MCP usage per agent/server/tool.
- **`user_memories`**: legacy memory table retained/migrated from earlier schema versions.

#### Permissions and notices

- **`permission_grants`**: durable allow/deny decisions per agent/tool (`agent_id`, `tool`, `mode`).
- **`permission_notices`**: audit/notice records for permission decisions and other system notices (including MCP-related settle states), with fields such as `kind`, `decision`, `command`, and text.

#### Scheduler

- **`scheduled_tasks`**: routines and reminders (`kind`, title/prompt, schedule JSON, `next_run_at`, last run/status, paused flags, timestamps).
- **`scheduled_runs`**: execution records linked to tasks (`status`, error, `started_at`, `completed_at`, related message/run context).

#### MCP and agent coordination

- **`mcp_connections`**: connected MCP server/account records (server id, label, account/expected email, scopes, status).
- **`agent_connections`**: per-agent binding to MCP connections with access level (`read`, `draft`, `full`).
- **`agent_messages`**: agent-to-agent/thread mailbox messages used for delegation workflows (status/queue/delivery fields).
- **`agent_skills`**: enabled/disabled skills per agent.
- **`agent_run_metrics`**: per-run metrics (rounds, tokens, tool calls, duration, status).

#### Account and app settings

- **`account_entitlements`**: cached entitlement payload and fetch timestamp for offline boot/grace behavior.
- **`app_settings`**: key/value settings used by the app (for example tenant ownership and UI/app preferences).

## OS keyring

Service name: `argus` (`Keyring/keyring.go`).

- **Account key:** `account/key` (paired desktop agent key).
- **MCP sessions/tokens:** refs such as `mcp/<serverID>/session` or connection-specific session refs; store serialized OAuth/session/token material.

Secrets are never stored as plaintext in SQLite and are not exposed to the frontend.

## Disk storage

### App config/data paths

Base config directory: `os.UserConfigDir()/argus` (platform-specific).

- **Database:** `argus.db`
- **Uploads:** `uploads/<attachment-id><ext>`
  - Created with restrictive permissions.
  - Validated for size/type; extracted text stored in SQLite for supported formats.
  - Deleted if DB insert fails; removed best-effort when attachments/purged data are cleaned up.
- **Deck files:** `decks/<id>.pptx` for generated slide deck artifacts.
- **Browser profile:** `browser/` by default (Argus-owned Chrome profile). Can be relocated via `ARGUS_BROWSER_PROFILE_DIR` (joined with `browser`).

### Cache paths

Base cache directory: `os.UserCacheDir()/argus` (platform-specific).

- Used by the catalog store/background refresh and related cached data.

### Configuration and credential files

Depending on environment/setup, Argus may read local files such as:

- `.env` files when `ARGUS_LOAD_DOTENV=1` (development-only, opt-in)
- MCP credential/config files in repo/config or user config locations (for connector OAuth client configuration, as implemented by MCP services)

These are inputs/configuration sources, not the primary app database.

## In-memory state

Runtime-only state includes:

- Shared core handles (DB, overlay, catalog store, event listeners)
- Active chat runs, cancellation handles, streaming/live tool sessions
- Pending interactive cards/requests (permissions, ask-user, MCP connect, browser takeover)
- Live MCP server/client sessions and connection bookkeeping
- Browser process/tab/page state and idle timers
- Catalog and other service caches held in memory

In-memory state is rebuilt from durable stores, keyring secrets, and runtime initialization when needed.

## Data flows (common cases)

- **Chat turn:** frontend sends message -> chat service validates and persists user/assistant rows -> builds prompt/history -> calls router/model -> streams events -> persists tool rounds/final message and model request telemetry.
- **Upload:** file bytes received -> validated and written to `uploads/` -> text extracted -> `attachments` row inserted -> file used in chat context.
- **Artifact save/export:** content saved to `artifacts` (and versions) -> optional generated file written (e.g. deck PPTX) -> export rendered to a user-selected path on demand.
- **MCP connect:** UI flow/agent request -> OAuth/session established -> secrets saved to keyring -> connection/binding metadata saved to SQLite -> tools become available per agent/access tier.
- **Scheduled task:** task persisted in `scheduled_tasks` -> scheduler fires due work -> run recorded in `scheduled_runs` -> unattended chat wake executed.

## Retention, recovery, and cleanup

- **WAL + busy timeout** improve reliability under concurrent reads/writes with a single writer connection.
- **Interrupted recovery:** in-flight streaming/started records can be marked interrupted on startup; data is preserved.
- **Artifact/version limits:** artifact bodies and version history are bounded/pruned by storage/service rules.
- **Attachment cleanup:** deleting an attachment or purging account data removes the DB row and best-effort deletes the on-disk file.
- **MCP cleanup:** disconnect/purge paths remove relevant keyring session secrets.

## Account sign-out and tenant change

- **Sign-out:** deletes the account key from the keyring, clears cached entitlements, and removes the router provider override.
- **Different account paired:** triggers tenant isolation. `Storage.PurgeAccountTables()` wipes account-scoped data (messages, memories, artifacts, attachments rows, schedules, permissions/notices, profiles/selections, MCP bindings/connections, etc.), removes known attachment files and MCP keyring secrets, clears cached entitlements, and reseeds required default/system profiles. The router provider override is removed and the new tenant is recorded.

This separation ensures account-scoped local content does not carry over to a different paired account on the same machine.
