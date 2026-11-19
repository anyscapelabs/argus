# FAQ

Answers to "where/why/what happens if" questions about Argus. Each answer links
to the page with the details.

## Where does Argus store my data?

Structured app state in a SQLite database at `os.UserConfigDir()/argus/argus.db`
(WAL mode, versioned migrations). Uploaded files, deck exports, and the browser
profile are files under the same config directory; the catalog cache lives under
the user cache directory. Secrets never touch the database — they are in the OS
keyring. See the [storage reference](reference/storage.md).

## Where are secrets kept?

In the OS keyring: the paired account key under the `argus` service, and MCP
OAuth/session tokens per connection. The database and the frontend never receive
raw secret values. See [security](concepts/security.md).

## What happens when I sign out or pair a different account?

Sign-out removes the key, the cached entitlements snapshot, and the router
override. Pairing a *different* account additionally wipes account-scoped local
data (chats, memories, artifacts, attachments, schedules, permissions, MCP
bindings, skills), deletes known attachment files and MCP keyring secrets, and
reseeds the default profiles — so one account never inherits another's local
workspace. See [architecture](concepts/architecture.md) and
[security](concepts/security.md).

## Can I use Argus offline?

Partially. Entitlements are cached in SQLite and served within a 7-day offline
grace period. Chat requires model access, so it needs connectivity (or a
locally-configured provider). Auth failures clear the entitlement cache rather
than reusing it.

## Which model handles my requests?

The router picks a model per turn based on your slot assignments (`main`,
`cheap`, `fallback`) and each model's declared capabilities, constrained by your
gateway entitlements. Details: [gateway models](concepts/gateway/models.md).

## Why does the agent ask permission for some things but not others?

Tools are grouped into permission families (`execute_command`, `file_access`,
`browser_use`, `browser_action`, `skill_install`, ...). Sensitive actions need a
grant or interactive approval; grants are durable per agent and tool. Host- and
scope-aware rules narrow what a grant covers. See
[security](concepts/security.md) and the [tools reference](reference/tools.md).

## Why did my scheduled task not do what I asked?

Scheduled runs are **unattended**: permission requests are held rather than
awaited — the action is not taken, a message explaining what was needed appears
in the chat, and replying lets the agent ask permission and try again. `ask_user`
is still refused. See the [scheduler guide](guides/scheduler.md).

## Why is the document I asked for in the artifacts panel?

Documents and decks are delivered only as artifact cards backed by the
`artifacts` store — the agent is instructed not to paste long documents into
chat, so there is one source of truth per document. See
[features](reference/features.md) and the [chat pipeline](concepts/chat-pipeline.md).

## Why doesn't the agent "know" my MCP tools without me saying anything?

MCP tool schemas are loaded progressively: the agent discovers tools with
`search_tools` and invokes them with `invoke_tool`, subject to the connection's
per-agent access tier (`read`/`draft`/`full`). Skills are similarly loaded on
demand via `read_skill`. See the [MCP guide](guides/mcp-integrations.md) and
[skills and memory](guides/skills-and-memory.md).

## Does the agent use my personal browser?

No. It drives its own Chrome instance with an Argus-owned profile under the
config directory, so cookies and logins persist across runs but stay isolated
from your normal browser. For logins/CAPTCHAs it offers you the window
(takeover). See the [browser guide](guides/browser-automation.md).

## A message shows as "interrupted" or "streaming" after a crash — is that a bug?

No. On startup, storage marks stranded in-flight work (messages stuck in
`streaming`, model requests stuck in `started`) as interrupted without deleting
your data. See the [storage reference](reference/storage.md).

## Why did my reaction to a message matter?

Reactions are stored and used as feedback signals alongside memory items,
avoid rules, and learned tool lessons — they shape future behavior. See
[skills and memory](guides/skills-and-memory.md).

## How do I change the gateway the app talks to?

Build-time `VITE_ANYSCAPE_GATEWAY_URL` only sets the pairing origin baked into
the frontend. At runtime, model traffic follows the gateway the key paired
against (persisted as a provider base URL override). See the
[configuration reference](reference/configuration.md).
