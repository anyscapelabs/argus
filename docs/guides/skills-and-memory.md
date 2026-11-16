# Skills and memory

Two different mechanisms help the agent do the right thing over time:

- **Skills** are static, versioned instructions shipped with the app, loaded
  on demand.
- **Memory** is learned, per-agent state accumulated from conversations and
  feedback.

Both are per-agent and durable, and both are wiped when a different account is
paired (see [storage](../reference/storage.md)).

## Skills

Skills are packaged instruction bundles: a `SKILL.md` plus supporting content
under `Services/Skills/data/<id>/`. The catalog (`Services/Skills/data/skills.json`)
lists each skill's id, name, description, associated connectors, and whether it
auto-enables when its connector is present.

Examples in the repo: `gmail`, `google-calendar`, `google-drive`, `github`,
`linear`, `notion`, `atlassian`, `cloudflare`, `sentry`, plus business skills
such as `finance`, `marketing`, `sales`, `accounting`, and `customer-support`.

### Progressive loading

Skills are **not** injected into every prompt. The agent:

1. Sees which skills are enabled for it (`agent_skills`).
2. Calls `read_skill` to load a skill's instructions when the task matches.
3. May call `install_skill` to enable a skill, which is permission-gated by the
   `skill_install` family ([security](../concepts/security.md)).

This keeps the base prompt small and only pays the token cost for the skill a
turn actually needs. `SKILL.md` files also contain "gotchas" sections that
record tool-specific pitfalls (for example GitHub's separate `owner`/`repo`
fields) so the agent avoids them.

### Where to change what

- Add a skill → new `Services/Skills/data/<id>/SKILL.md` plus a `skills.json`
  entry.
- Change enablement rules → `Services/Skills/skills.go` and `agent_skills`.

## Memory

Durable memory is more than a key/value store. Tables (see
[storage](../reference/storage.md)):

| Table | Role |
|---|---|
| `memory_items` | The main durable memory store: `agent_id`, `key`, `value`, `scope`, `confidence`, source/revision metadata, timestamps. |
| `memory_exposures` | Which memories were shown to which model request, with position/score/token estimates. |
| `memory_avoid_rules` | Things the agent should avoid doing. |
| `learned_tool_lessons` | Lessons from tool/MCP usage, per agent/server/tool. |
| `message_reactions` | User emoji/signal feedback on messages, used as a memory signal. |
| `user_memories` | Legacy key/value table retained for migration/history. |

### How memory is used

- **Writing:** the `remember` tool stores memory items; `save_agent_note` stores
  agent-private notes; reactions and outcomes feed lessons and feedback.
- **Reading:** `recall_memories` retrieves items; chat also injects relevant
  memories into the prompt during context building (`Services/Chat/memory_context.go`).
- **Forgetting:** `forget` deletes items, and avoid rules constrain future
  behavior.

Memory is scoped per agent. The hidden `__global_memory__` profile is the owner
used by the shared memory system, and it is reseeded after an account purge
alongside the `default` profile.

### Exposure tracking

`memory_exposures` records what the model actually saw, which matters for
debugging "why did the agent say that?" questions and for cost accounting —
memory injected into a request is part of the context size recorded in
`model_requests`.

## Where to change what

- New memory table or field → `Storage/migrations.go`, then update the
  [storage reference](../reference/storage.md) and account purge coverage.
- Prompt injection of memory → `Services/Chat/memory_context.go` and
  `Services/Chat/prompt.go`.
- Memory tools → `tooldefs_memory.go` / `tool_memory.go`, then the
  [tools reference](../reference/tools.md).
