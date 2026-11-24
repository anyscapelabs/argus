# Skills and memory

Two different mechanisms help the agent do the right thing over time:

- **Skills** are static, versioned instructions shipped with the app, loaded
  on demand.
- **Memory** is learned, per-agent state accumulated from conversations and
  feedback.

Both are per-agent and durable, and both are wiped when a different account is
paired (see [storage](../reference/storage.md)).

## Skills

Skills are packaged instruction bundles. The source of truth is `skills/<id>/`:

- `SKILL.md` — the author's instructions, stored **verbatim** (no rewriting or
  summarizing). Frontmatter is stripped only when the body is served to the
  agent.
- `skill.json` — registry metadata: id, name, trigger description, version,
  connectors, `allowed_tools`, auto-enable rule, channel, publisher and source
  attribution.

`Services/Skills/data/` is the generated offline floor, and `skills.json` inside
it is generated from every `skill.json`. Never edit `Services/Skills/data/` by
hand: run `go run ./tools/gen-skills` and commit the result. CI verifies the
baseline matches `skills/`.

The registry is published by `.github/workflows/publish-skills.yml`: it lints
the bundles, builds immutable `<id>@<version>.json` files plus a signed
`index.json` (Ed25519), and uploads them to S3 behind CloudFront at
`https://registry.anyscapelabs.com`. The desktop verifies the index signature
with an embedded public key and each bundle's sha256 before use. The signed
index replaces the embedded baseline on startup and every 24h; the app keeps
the previous copy as a cache for offline use.

The collection includes the original 22 built-ins plus role-based skills
(executive, finance, marketing, product, legal, engineering, design, data) and
communication-style skills. Sources and licenses are recorded in
[`skills/ATTRIBUTION.md`](../../skills/ATTRIBUTION.md).

### Progressive loading

Skills are **not** injected into every prompt. The agent:

1. Sees installed skills' one-line descriptions in the system prompt.
2. Calls `search_skills(query)` to discover the rest of the registry.
3. Calls `read_skill` to load a matched skill's full, original instructions.
4. May call `install_skill` first, which is permission-gated by the
   `skill_install` family ([security](../concepts/security.md)) and downloads
   and verifies the bundle before enabling it.

`allowed_tools` only unlocks tool discovery in the session — it never skips a
permission check. The agent can install a skill mid-conversation; no desktop
release is needed.

### Where to change what

- Add or update a skill → edit `skills/<id>/SKILL.md` (verbatim) and
  `skills/<id>/skill.json` (bump `version`), then regenerate the baseline.
- Change enablement rules → `Services/Skills/skills.go` and `agent_skills`.
- Change the CDN URL → `DefaultIndexURL` in `Services/Skills/skills.go` or
  `ARGUS_SKILLS_INDEX_URL` for a staging build.

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
