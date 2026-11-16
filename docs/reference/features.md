# Services and features

This document summarizes the main features in Argus and the backend/frontend pieces that implement them.

## Overview

Argus combines a local desktop UI with a Go backend that can run agent workflows, store durable state, call models through the gateway, and use tools such as files, terminal commands, browser automation, MCP integrations, and scheduling.

## Chat and agent runs

**Primary areas:** `Services/Chat/*`, `Storage/messages.go`, `Storage/model_requests.go`, `frontend/src/pages/Chat.tsx`, `frontend/src/domains/chat/*`

- Persistent chat history per agent with paginated reads and message sequencing.
- Streaming assistant responses and run lifecycle events (`chat:run_started`, `chat:run_finished`, `chat:typing`, `chat:phase`, `agent:activity`, `chat:message`).
- Agent loop that can run multiple model/tool rounds, persist tool rounds, handle retries/compaction, and finalize results with usage/cost/status.
- History replay and repair for provider-safe tool-call/result continuity.
- Conversation-aware context building: recent history, attachments, memories, skills, integrations, and tool definitions.
- Reactions on messages and read/status APIs for chat state.
- Support for special turn kinds such as steering, scheduled wakes, delegated messages, tool rounds, and file-write-related records.

## Tools and capabilities

**Primary areas:** `Services/Chat/tool_*`, `Services/Chat/tooldefs_*`, `Services/Chat/mcp_gateway.go`, `Services/Terminal`, `Services/Search`, `Services/Memory`, `Services/Slides`, `Services/Location`

Argus exposes a mix of direct tools and discoverable/lazy-loaded tools. Depending on configuration and permissions, agents can:

- Read and work with conversation/context data
- Search the web (when available)
- Work with files and terminal commands (permission-gated)
- Create and update artifacts/slides
- Use memory-related tools
- Schedule tasks
- Connect to and use MCP integrations
- Use browser automation tools
- Ask the user for input/clarification during a run

Tool execution is routed through chat dispatch with activity events, result handling, and safety checks.

## Permissions

**Primary areas:** `Services/Permission/*`, `Storage/grants.go`, `Storage/notices.go`, `frontend/src/pages/Permissions.tsx`, `frontend/src/domains/permission/*`

- Global and per-agent permission policies for sensitive capabilities.
- Supported capability families include terminal commands, file access, browser actions (including host-scoped grants), skill install, and browser use.
- Interactive approval flow with request/response events and durable allow decisions when chosen.
- Unattended runs (such as scheduled tasks) do not wait for interactive approval; restricted actions are refused and recorded.
- Permission notices provide an audit trail of decisions and related events.

## MCP and integrations

**Primary areas:** `Services/MCP/*`, `Services/MCPConnect/*`, `Services/Integrations/*`, `Storage/mcp_connections.go`, `frontend/src/pages/Integrations.tsx`, `frontend/src/pages/IntegrationDetail.tsx`, `frontend/src/domains/mcp/*`

- Embedded integration catalog describing supported connectors.
- Connect/bind/reauth flows with UI cards and event updates.
- Local stdio and remote OAuth-backed MCP support (implementation-dependent per connector).
- Multiple connections/accounts per server and per-agent bindings with access tiers (`read`, `draft`, `full`).
- Token/session persistence in the OS keyring and connection metadata in SQLite.
- MCP tool discovery and invocation exposed to chat through a controlled gateway.

## Browser automation

**Primary areas:** `Services/Browser/*`, `Services/Chat/browser.go`, `Services/Chat/takeover.go`, `frontend/src/domains/browser/*`, `frontend/src/pages/BrowserSettings.tsx`

- Argus-owned Chrome/CDP browser session for agent use.
- Navigation, page reading/snapshots, captures, and automated actions.
- Persistent browser profile stored separately from the user’s normal browser.
- Idle shutdown and app-managed lifecycle.
- Human takeover flow for logins, CAPTCHAs, confirmations, or sensitive steps, surfaced in the UI.
- Browser settings and permission controls to enable/disable or scope access.

## Scheduler and background tasks

**Primary areas:** `Services/Scheduler/*`, `Storage/scheduled_tasks.go`, `frontend/src/domains/scheduler/*`

- Durable routines and reminders stored in SQLite (`scheduled_tasks`, `scheduled_runs`).
- Due-task scanning, coalescing/backoff behavior, and recovery of missed work after restarts/offline periods.
- Scheduled runs execute as unattended chat wakes.
- Schedule changes and run outcomes emit events/notifications.

## Artifacts

**Primary areas:** `Services/Artifacts/*`, `Storage/artifacts.go`, `frontend/src/domains/artifacts/*`, `frontend/src/pages/ArtifactViewer.tsx`

- Save and version documents and slide decks.
- Canonical artifact content stored in SQLite with metadata and history.
- Deck artifacts can generate and store PPTX files on disk.
- Export artifacts to user-selected formats/paths (as supported by the export pipeline).
- UI support for opening/viewing artifacts from chat and other flows.

## Uploads and attachments

**Primary areas:** `Services/Uploads/*`, `Storage/attachments.go`, `frontend/src/domains/uploads/*`

- Upload validation (type/size) and secure local storage under the app config directory.
- Text extraction for supported document formats to make content available to chat.
- Attachment metadata and extracted text stored in SQLite; binary files stored on disk.
- Attachment listing/deletion and preview support in the UI.

## Memory and personalization

**Primary areas:** `Services/Memory/*`, `Storage/user_memories.go`, `Storage/migrations.go` (memory tables), `Services/Skills/*`

- Structured durable memories (`memory_items`) with scope/confidence/source metadata.
- Memory exposure tracking for model requests and feedback/reaction signals.
- Avoid rules and learned tool lessons.
- Skills system for progressively loading specialized instructions/content per task/agent.
- Project/user preferences may also be guided by local instruction files (for example `.commandcode/taste/taste.md` in the workspace environment).

## Voice and live interactions

**Primary areas:** `Services/Chat/live*.go`, `Services/Chat/voice_call.go`, `frontend/src/domains/live/*`, chat UI components

- Live/voice session configuration and tool gateway integration.
- Voice call transcript persistence and rendering in chat.
- Real-time interaction support coordinated through backend services and frontend hooks/components.

## Account, pairing, and entitlements

**Primary areas:** `Services/Account/*`, `Keyring/*`, `frontend/src/pages/SignIn.tsx`, `frontend/src/pages/Onboarding.tsx`, `frontend/src/pages/Profile.tsx`, `frontend/src/pages/Billing.tsx`, `frontend/src/domains/account/*`

- Device pairing with an agent key stored in the OS keyring.
- Entitlements verification against the gateway and cached offline access within grace period.
- Account/profile views and plan-related UI (including billing/settings surfaces).
- Sign-out and tenant isolation behavior to prevent cross-account data leakage.

## Models, providers, and catalog

**Primary areas:** `Services/Provider`, `Services/Models`, `Services/Catalog`, `Catalog/*`, `Provider/*`, `Router/*`, `Protocol/*`, `docs/concepts/gateway/*`

- Embedded provider overlay for the routed gateway provider.
- Catalog refresh/storage and model discovery/selection.
- Model slots, selections, and per-agent model configuration.
- Request routing and protocol adapters for model execution.

## Frontend experience

**Primary areas:** `frontend/src/App.tsx`, `frontend/src/pages/*`, `frontend/src/components/*`, `frontend/src/domains/*`

- Main pages/flows: sign-in/onboarding, home, chat, integrations, settings (appearance, notifications, browser, permissions, usage, profile, billing).
- Chat transcript UI with cards for permissions, MCP connect, browser takeover, ask-user, artifacts, and voice-related content.
- React Query-based data fetching/caching with event-driven invalidation from backend runtime events.
- Theme/accent support and desktop-oriented layout (sidebar, toolbar, overlays).

## Cross-cutting concerns

- **Eventing:** backend emits domain events bridged to the frontend and in-process listeners.
- **Durability:** key workflows persist state in SQLite for recovery and auditing.
- **Safety:** permissions, access tiers, and takeover/approval flows gate risky actions.
- **Isolation:** secrets in keyring, account-scoped local data purge on tenant change.
- **Extensibility:** MCP integrations and skills allow capabilities to grow without hardcoding every tool into the core UI.
