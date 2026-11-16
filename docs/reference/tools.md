# Tools reference

Catalog of the built-in tools the agent can call, as defined by the
`tooldefs_*.go` files in `Services/Chat` and dispatched by `Services/Chat/tool_dispatch.go`.
MCP tools are not listed here; they are discovered per connection and exposed
through the MCP gateway (see [MCP and integrations](../guides/mcp-integrations.md)).

Tool access is checked against [permissions](../concepts/security.md) before
side effects occur. Sensitive tools require a permission grant or interactive
approval; unattended runs (scheduled wakes) refuse interactive approval instead
of waiting.

## Files and terminal

| Tool | What it does | Permission |
|---|---|---|
| `execute_command` | Runs one shell command on the user's machine with `/bin/sh`, with an optional timeout (1–300s, default 60). | `execute_command` family |
| `read_file` | Reads one text file, paged by lines; returns content plus total line count. | `file_access` |
| `write_file` | Creates or replaces a text file (up to 1 MB), creating parent folders; supports append mode. | `file_access` |
| `edit_file` | Replaces exact `old_text` with `new_text` atomically, with a required match count. | `file_access` |
| `grep_files` | Searches files under one folder for a Go regular expression, with an optional filename glob. | `file_access` |

## Web and browser

| Tool | What it does | Permission |
|---|---|---|
| `web_search` | Searches the public web through the Anyscape Gateway when the plan allows, with free SearXNG fallback; returns titles, URLs, snippets. | plan-dependent |
| `browser` | Drives a real browser by its accessibility tree (open, read, click, fill, press, scroll, find, tabs, JS, profile import); each call returns an accessibility snapshot. | `browser_use`; sensitive actions also hit `browser_action` |

## Memory

| Tool | What it does |
|---|---|
| `remember` | Saves a durable memory item for the agent. |
| `forget` | Deletes a memory item. |
| `recall_memories` | Reads back stored memories for context. |
| `save_agent_note` | Saves an agent-private note (part of the `memory_items` system). |

## Scheduling

| Tool | What it does |
|---|---|
| `schedule_reminder` | Creates a one-time reminder the app fires even if the chat is closed. |
| `create_routine` | Creates a recurring routine (see [scheduler guide](../guides/scheduler.md)). |
| `list_scheduled` | Lists the agent's scheduled tasks. |
| `cancel_scheduled` | Cancels a scheduled task. |

## Documents and artifacts

| Tool | What it does |
|---|---|
| `write_artifact` | Saves or extends a document or slide deck in the `artifacts` store; the artifact card is the only delivery surface for documents (see [chat pipeline](../concepts/chat-pipeline.md)). |
| `create_slides` | Creates a slide deck artifact. |

## Conversation and self

| Tool | What it does |
|---|---|
| `ask_user` | Pauses the run to ask the user a question; refused in unattended contexts. |
| `conversation_read` | Reads conversation history (search/list with pagination). |
| `list_messages` | Lists messages, including other threads where permitted. |
| `delete_message` | Deletes a message. |
| `get_time_and_location` | Returns current time and the user's location context. |
| `install_skill` | Installs/enables a skill (permission-gated via `skill_install`). |
| `read_skill` | Loads a skill's instructions progressively (progressive disclosure). |

## Agents and delegation (multi-agent)

| Tool | What it does |
|---|---|
| `list_agents` | Lists the other agents on the device. |
| `message_agent` | Sends a message to another agent/thread (delegation via `agent_messages`). |
| `reply` | Replies within a delegated thread. |
| `set_my_role` | Sets the agent's role. |

## MCP gateway

| Tool | What it does |
|---|---|
| `connect_mcp` | Starts a connect/bind flow for an MCP integration. |
| `search_tools` | Discovers MCP tools available to the agent, so schemas are loaded progressively rather than attached wholesale. |
| `invoke_tool` | Invokes a discovered MCP tool (subject to access tiers). |
| `gmail__create_draft` / `send_email` / `create_draft` | Examples of connector-specific tools exposed through the gateway. |

## Notes for contributors

- Tool definitions live in `tooldefs_*.go` (schema) and `tool_*.go` /
  `*_tool.go` (handlers); a handler is reached from `runTool` via a single
  dispatch line — keep `runTool` a router.
- Tool results are persisted as `tool_round` message rows; handlers must not
  write chat rows themselves (see [chat pipeline](../concepts/chat-pipeline.md)).
- If you add a tool that mutates state, decides whether it belongs in a
  permission family and update `Services/Permission` accordingly.
