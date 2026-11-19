# Security

This document describes the security boundaries and safeguards in Argus: how secrets are stored, how the app pairs with the gateway, how permissions work, and how local data is isolated between accounts.

## Trust boundaries

Argus runs as a local desktop app (Wails v3 backend in Go, React frontend). It talks to:

- **The Anyscape Gateway** for account entitlements and model traffic (through the embedded `argus-router` provider).
- **MCP servers/connectors** the user enables (local stdio servers or remote OAuth-backed services).
- **Websites** when browser automation is used.
- **The local machine** through tools (files, terminal, browser profile, scheduler, etc.) subject to permission checks.

The frontend receives status and data needed for the UI, but not secret material such as API keys or MCP tokens.

## Secrets and credentials

### Account key

- The paired desktop agent key is stored in the OS keyring under service `argus`, ref `account/key` (`Keyring/keyring.go`, `Services/Account/service.go`).
- The frontend never receives the raw key; it only sees pairing/account status and errors.
- The key is sent to the gateway when verifying entitlements (`/api/v1/account/entitlements`) and for authenticated gateway/model flows as designed by the router/account integration.

### MCP tokens and sessions

- OAuth/session material for MCP connections is stored in the OS keyring (`Services/MCP/tokens.go`), per server/connection (for example `mcp/<serverID>/session` or connection-specific refs).
- Stored data can include client/session metadata, scopes, access tokens, refresh tokens, and expiry information.
- SQLite stores MCP connection metadata and per-agent bindings (`mcp_connections`, `agent_connections`), not the token secrets themselves.

### Provider/config metadata

- `provider_configs` in SQLite stores provider configuration metadata (such as secret refs, key hints/last4, validation timestamps, base URL overrides). It does not store plaintext secret values.
- Legacy per-provider key refs are supported in keyring helpers for compatibility, but the router/account key model is the primary path.

### Development-only credential loading

- `ARGUS_LOAD_DOTENV=1` enables opt-in loading of `.env` files for local OAuth client IDs/secrets during development (`Services/MCP/service.go`).
- This is off by default in packaged builds. Treat `.env` and local credential files as sensitive development inputs. See the [configuration reference](../reference/configuration.md) for the full environment variable list.

## Pairing, entitlements, and offline access

- Pairing verifies the agent key against the gateway and caches the entitlements snapshot in SQLite (`account_entitlements`) for offline use (`Services/Account/service.go`, `Storage/account_entitlements.go`).
- Cached entitlements may be served when the gateway is unreachable within the offline grace period (7 days), and the account view is marked offline.
- Authentication failures (invalid/revoked/expired key, verification failures) do not reuse cached entitlements; cached data is cleared on auth failure paths.
- On successful pairing/verification, Argus records the provider base URL override for routed model traffic so requests go to the backend the key paired with.

## Account isolation and local data wipe

Argus treats local account-scoped data as belonging to the current paired tenant.

- The current tenant is tracked in `app_settings` (`account/tenant_user_id`, `Services/Account/tenant.go`).
- **Sign-out** removes the account key from the keyring, clears cached entitlements, and removes the router provider override. It does not wipe the tenant’s local workspace by itself.
- **Pairing a different account** triggers isolation: account-scoped tables are purged (`Storage.PurgeAccountTables()`), the router provider override is removed, and the new tenant is recorded.

### What purge covers

Before database deletion, purge best-effort removes:

- Known attachment files from disk (paths recorded in `attachments`)
- MCP session secrets from the keyring for known connections (and legacy server refs)

Then it deletes account-scoped data including chat history/messages, model requests, memories/reactions/exposures/avoid rules/lessons, artifacts and versions, attachments rows, scheduled tasks/runs, permission grants/notices, model/profile/selection state, MCP connections and agent bindings, skills/metrics, agent profiles/counters, and cached entitlements. Required default profiles (`default`, `__global_memory__`) are reseeded.

This prevents one account’s chats, memories, artifacts, and permissions from leaking into another account on a shared machine.

## Permissions and approvals

`Services/Permission/service.go` enforces runtime checks for sensitive capabilities.

### Tool families

The built-in tools and their permission families are cataloged in the
[tools reference](../reference/tools.md). The families are:

- `execute_command` (terminal commands)
- `file_access` (read/write/edit outside the conversation context as defined by the tool path)
- `browser_action` (sensitive browser actions such as submit/send/purchase/post/delete; can be host-scoped)
- `skill_install` (enabling/installing skills)
- `browser_use` (interactive browser use; defaults to allowed as a kill switch unless blocked by policy)

### Policy behavior

- Policies can be global (`*`) and per-agent, with modes like ask/allow/deny and agent inheritance from global defaults.
- Durable grants are stored in `permission_grants`; decisions/notices are recorded in `permission_notices`.
- `Request(...)` checks stored grants first. If no grant applies:
  - **Interactive runs** emit `permission:requested`, wait for a user response, and can persist allow-always decisions.
  - **Unattended runs** (for example scheduled/background execution) hold the action, persist an actionable notice shown in the chat, and return a `deferred` decision instead of blocking indefinitely. The agent reports what it needed; replying lets it ask permission and retry on the next interactive turn.
- Browser actions support host-specific and family/global resolution rules, so approvals can be scoped appropriately.

## MCP and integration safety

- MCP connections are bound per agent with access tiers (`read`, `draft`, `full`) stored in `agent_connections`.
- Chat/tool integration exposes MCP tools through a controlled gateway/discovery path rather than blindly attaching everything.
- Connection flows can require user approval in the UI, verify expected account/email identity where applicable, and surface settle/reauth states via notices and events.
- Tokens stay in the keyring; disconnect/reconnect and account purge paths clean up stored session secrets.

## Browser automation safety

- Browser automation uses an Argus-owned Chrome profile (`os.UserConfigDir()/argus/browser` by default, or `ARGUS_BROWSER_PROFILE_DIR`), separate from the user’s normal browser profile.
- The browser service manages session state, tabs, idle shutdown, and takeover/blocked states.
- Sensitive or irreversible actions are designed to go through permission checks and/or human takeover flows in the chat/browser tool path.
- Takeover requests and status are surfaced to the UI so the user can complete steps like logins, CAPTCHAs, or confirmations when needed.

## Data at rest and local files

Argus keeps structured state in SQLite, secrets in the OS keyring, user-facing
files on disk, and runtime state in memory. The full table-by-table schema,
keyring refs, disk paths, and cleanup flows are in the
[storage reference](../reference/storage.md); the security-relevant points are:

- The database contains app state, history, metadata, and cached entitlements — never keyring secrets.
- Uploads are written with restrictive file permissions.
- Temporary files may be used during exports, uploads, or browser operations, but durable user data stays in the documented locations.

## Network and model traffic

- Model/provider traffic is routed through the embedded `argus-router` provider toward the paired Anyscape Gateway.
- Plan/model allowlists are enforced server-side; the client should be treated as fail-closed with respect to gateway authorization decisions.
- `frontend/.env` values such as `VITE_ANYSCAPE_GATEWAY_URL` are build-time configuration; the effective paired gateway/base URL used for traffic can be persisted at pair time.

## Logging and diagnostics

- The app emits runtime events for UI/state coordination (chat, permissions, MCP connect, schedule, etc.; see the [events reference](../reference/events.md)).
- Notices and request records (for example permission notices and model request telemetry) are stored to support auditing and debugging. Avoid logging or persisting raw secrets; key material belongs in the keyring.

## Security-relevant practices for contributors

- Do not expose secrets to the frontend or write them into SQLite, artifacts, or logs.
- Keep permission checks in the execution path for tools that mutate state, access files/terminal, or perform sensitive browser actions.
- Preserve account isolation behavior when adding new account-scoped tables, files, or keyring entries; update purge/cleanup logic accordingly.
- Regenerate bindings after backend API changes and keep docs in sync for security-sensitive behavior.
- Prefer explicit, reviewable defaults: deny or ask for risky actions, and fail safely on auth/verification errors.
