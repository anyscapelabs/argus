# MCP and integrations

This guide walks through how MCP (Model Context Protocol) integrations work in
Argus end to end: what is stored where, how connections are established, and how
tools reach the agent. The feature catalog is in [features](../reference/features.md).

## The pieces

| Piece | Where it lives |
|---|---|
| Integration catalog (supported connectors) | `Services/Integrations` |
| Connection/session management, tool discovery and calls | `Services/MCP` |
| User-facing connect/bind/reauth flows and cards | `Services/MCPConnect` |
| Durable connection + per-agent binding rows | `mcp_connections`, `agent_connections` (SQLite) |
| OAuth/session secrets | OS keyring (`Services/MCP/tokens.go`) |
| UI | `frontend/src/pages/Integrations.tsx`, `IntegrationDetail.tsx`, `frontend/src/domains/mcp` |

## How a connection is established

1. The user (or the agent via `connect_mcp`) starts a connect flow for a
   connector from the embedded integration catalog.
2. `MCPConnect` shows a connect card in chat (`mcp:connect:requested`) and runs
   the auth flow — OAuth for remote servers, local setup for stdio servers.
   Flows can require user approval and verify the expected account/email
   identity where applicable.
3. On success, secrets go to the keyring, connection metadata goes to
   `mcp_connections`, and `integrations:changed` fires. `MCPConnect.OnSettled`
   steers the chat service so the agent can react to the new connection.
4. Disconnect/reauth reuse the same card mechanism (`mcp:connect:status`,
   `mcp:connect:resolved`), keeping one live card per agent where appropriate.

## How MCP tools reach the agent

Argus exposes MCP capabilities progressively rather than attaching every tool
schema on every turn:

- The agent uses `search_tools` to discover what a connection offers and
  `invoke_tool` to call it, both through the controlled gateway in
  `Services/Chat/mcp_gateway.go`.
- Tools are named per connector (for example `gmail__create_draft`).

## Per-agent bindings and access tiers

`agent_connections` binds an agent to a connection with an access level —
`read`, `draft`, or `full`. Tool exposure and behavior respect the tier, so one
agent can be read-only against an integration while another has full access.
Enablement per agent is also tracked in `agent_skills`-style settings for
skills and in the integrations UI for connections.

## Safety and cleanup

- Tokens never leave the keyring; SQLite holds metadata only.
- Access tiers are enforced in the chat/tool path before side effects.
- Disconnect, reauth, and account purge paths remove the relevant keyring
  session secrets (`Services/MCP/tokens.go`).
- Development-only `.env` loading of OAuth client credentials requires
  `ARGUS_LOAD_DOTENV=1` (see [configuration](../reference/configuration.md)).

## Where to change what

- New connector → integration catalog entry plus `Services/MCP` support as
  needed (stdio vs remote OAuth).
- Changed credential storage → `Services/MCP/tokens.go`, and check the account
  purge cleanup path.
- Changed UI flow → `MCPConnect` events and the `mcp` frontend domain.
