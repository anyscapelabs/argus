# ADR 0001: Deterministic Action Execution for Email (Gmail Send)

- Status: Proposed
- Date: 2026-11-16
- Scope: Services/Chat, Services/Permission, Services/Shared, Storage, Protocol, frontend chat cards

## 1. Context

### 1.1 The incident

Reconstruction from the live database (`~/.config/argus/argus.db`, read-only) across
three conversation turns:

| Turn | Model requests | Tool calls | Drafts created | Sends attempted | Sends delivered | Final model claim |
|---|---|---|---|---|---|---|
| "termination" turn | 5 | — | 1 | 0 | 0 | **"sent"** (false) |
| CTO turn | 11 | — | 1 | 0 | 0 | **"sent"** (false) |
| "sent it not draft" turn | 28 | 27 (all local discovery/history) | 0 | 0 | 0 | ended `429 daily_limit_reached` |

Totals: ~44 model requests, ~41 tool calls, 2 drafts, **0 sends, 2 false success claims**.

Root causes, in order of severity:

1. **Capability mismatch.** The live Gmail connector exposes 23 tools and none of them
   send mail. Yet `Services/Skills/data/gmail/SKILL.md` advertises `gmail__send_message`
   and `Services/Integrations/data/catalog.json` claims "Draft and send emails". The model
   was told it could send, was given no send tool, and improvised: it either fabricated a
   success claim or burned 28 rounds hunting for a tool that does not exist.
2. **The model reports outcomes it did not observe.** The final-response instructions in
   `Services/Chat/prompt.go` let the model narrate results; nothing requires an executed
   action to be backed by a receipt before the model may claim success. A draft created in
   tool output became "I sent it" in prose.
3. **Loop guards do not stop semantic spinning.** `Services/Chat/send.go:29` sets
   `maxToolRounds = 250`; `Services/Chat/run_budget.go:21` sets `stuckRoundLimit = 8`, but
   the detector (agent_loop.go:145, 450) only fires on *identical call signatures* — varied
   discovery queries ("find email tool", "list gmail tools", "search inbox...") never match,
   so a turn escalated to 28 rounds with zero progress.
4. **Telemetry is too coarse to catch this in the act.** `agent_run_metrics` records
   per-run aggregates; no per-round, per-tool-call, or cost-capped interruption exists.

### 1.2 Current enforcement is tier-based, not action-based

- `Services/Chat/mcp_gateway.go` (`checkAccessTier`) enforces `read | draft | full` per
  connection: `read` allows read-only tools; `draft` allows read-only plus non-destructive
  annotated tools; `full` allows everything the server publishes. Unknown annotations fail
  closed for `read`/`draft` but `full` is the explicit escape hatch.
- The connection record (`Storage/mcp_connections.go`) for the incident account
  (`hello@anyscapelabs.com`) is `access_level=full`, scopes `gmail.modify` + `gmail.compose`.
  So even a hypothetical send tool would have executed with no per-action approval.
- Generic permission families (`Services/Permission/service.go`) cover terminal commands,
  files, browser action/use, and skill installation. There is **no email-send / consequential
  external-action family**, and no `Permission.Request` path exists for dynamic MCP tool
  invocation today — MCP execution relies solely on the access tier.
- The frontend has an unintegrated `EmailDraftCard.tsx`
  (`pending | sending | sent | denied`, send/deny callbacks) that is not rendered by
  `Chat.tsx`; only `PermissionCard`, `MCPConnectCard`, and `TakeoverCard` are live.
- `Protocol/canonical.go` blocks are `text | image | tool-use | tool-result | thinking`
  only; there is no authoritative action-status block the UI can trust.
- Migrations run through `migration22` (`Storage/migrations.go:716`); no action/receipt
  persistence exists.

### 1.3 Gmail API facts (external constraints)

- Official sending operations are `users.drafts.send` and `users.messages.send`
  (https://developers.google.com/workspace/gmail/api/reference/rest/v1/users.drafts/send,
  https://developers.google.com/workspace/gmail/api/reference/rest/v1/users.messages.send).
- The Gmail MCP server tool list (https://developers.google.com/workspace/gmail/api/reference/mcp)
  does not include a send tool in the deployment we are connected to (23 tools, none send).
- A successful `send` returns a `Message` resource. It is **not** a delivery confirmation,
  and Gmail provides **no client idempotency key**. Retrying a timed-out send can double-send.
- The granted scopes `gmail.compose` / `gmail.modify` are sufficient for
  `drafts.create` + `drafts.send` + `messages.send` directly via the REST API — we do not
  need the MCP connector for the send path.

## 2. Decision

**The model proposes actions; a deterministic orchestrator authorizes, executes, verifies,
and persists the outcome. The model never reports an outcome it did not observe, and never
carries a mutating external side effect itself.**

Concretely:

1. **Sends execute through a first-party adapter, not the MCP connector.** Argus calls
   Gmail REST (`users.drafts.create` → `users.drafts.send`) directly using the connection's
   stored OAuth tokens. Rationale: the connector cannot send; the scopes are already
   granted; a first-party adapter lets us own timeouts, retries, op-ids, and receipts —
   none of which we can retrofit onto a third-party MCP server. Reading and composing
   drafts may continue through the connector.
2. **An explicit action lifecycle replaces prose success claims.** Every consequential
   action (`email.send` first) is an `Action` row that moves through
   `requested → approved → running → succeeded | failed | unknown`. The final response is
   assembled from ledger state: `succeeded` only with a matching receipt/postcondition;
   timeout or ambiguous completion is `unknown` and is *reported as unknown* to the user,
   never as success.
3. **Approval is intercepted at the dispatch boundary, per action.** When the model emits
   a proposed `email.send` (a new typed tool, not a raw MCP call), the agent loop pauses,
   emits `action:requested` + a card, and blocks on the existing permission-gate machinery
   (`Services/Permission/service.go`). `full` access tier alone no longer authorizes
   consequential external sends.
4. **Reconciliation handles Gmail's lack of idempotency.** See §5.
5. **Budgets are enforced in the loop, not just recorded.** See §6.

## 3. Alternatives considered

| Alternative | Rejected because |
|---|---|
| Fix the connector's SKILL.md/catalog copy only (make the model stop trying) | Necessary but insufficient: it removes the capability lie, not the false-success pattern. Any future connector with a real send tool reintroduces the problem. |
| Rely on MCP tool annotations + access tiers (`draft` tier blocks send) | `full` tier bypasses everything and the incident account is `full`. Tier is a *connection-level* grant, not a per-action authorization; OWASP LLM06 (Excessive Agency) calls exactly this out — scope of authority must be enforced per action, not per session. |
| Prompt-engineering only ("never claim sent unless you see a send result") | Cheap and included, but instruction-following is probabilistic; the incident turn followed a system prompt already describing tool results and still fabricated. Verification must be structural. |
| Build sends into the connector by switching Gmail MCP deployments | Depends on Google shipping/exposing it; we still would not control timeouts, op-ids, or receipts. The adapter gives us the contract we need regardless of connector evolution. |
| Full agent-framework rewrite (LangGraph-style orchestration) | Out of proportion. The needed properties (checkpointed loop with bounded rounds, typed handoff between proposal and execution) can be added to the existing `agent_loop.go` incrementally; the referenced frameworks inform the design, not the runtime. |

## 4. Action lifecycle and ledger

### 4.1 States

```
requested ──approved──▶ running ──receipt──▶ succeeded
    │                     │
    ├──denied──▶ (terminal) ├──error──▶ failed
    │                     └──timeout/inconclusive──▶ unknown ──reconcile──▶ succeeded | unknown(final)
    └──expired (budget/policy invalidation)
```

Rules:

- `succeeded` requires a **receipt**: for `email.send`, the Gmail `Message` returned by
  `drafts.send`/`messages.send` (message id + thread id) plus a postcondition check.
- `unknown` is a first-class terminal-for-the-turn state. The model must tell the user
  "the send timed out and is being verified" — it may not say "sent".
- A new turn may consume an earlier `unknown` after reconciliation resolves it.

### 4.2 Schema (migration23)

```sql
CREATE TABLE actions (
  id            TEXT PRIMARY KEY,          -- uuid
  run_id        TEXT NOT NULL,             -- agent run
  message_id    TEXT NOT NULL,             -- chat message the turn belongs to
  kind          TEXT NOT NULL,             -- 'email.send' (extensible)
  status        TEXT NOT NULL,             -- requested|approved|denied|running|succeeded|failed|unknown|expired
  connection_id TEXT,                      -- gmail connection used
  account_email TEXT,
  payload_json  TEXT NOT NULL,             -- proposed action (to, subject, draft ref, etc.)
  op_id         TEXT NOT NULL UNIQUE,      -- deterministic idempotency token, uuid, generated once
  attempt       INTEGER NOT NULL DEFAULT 0,
  error_code    TEXT,                      -- 'daily_limit_reached', 'timeout', ...
  created_at, updated_at TIMESTAMP
);

CREATE TABLE action_receipts (
  id          TEXT PRIMARY KEY,
  action_id   TEXT NOT NULL REFERENCES actions(id),
  kind        TEXT NOT NULL,               -- 'gmail.send'
  body_json   TEXT NOT NULL,               -- provider response subset (message id, thread id, labels)
  observed_at TIMESTAMP NOT NULL
);

CREATE TABLE action_reconciliations (
  id          TEXT PRIMARY KEY,
  action_id   TEXT NOT NULL REFERENCES actions(id),
  method      TEXT NOT NULL,               -- 'draft_gone'|'sent_search'|'duplicate_detected'
  result      TEXT NOT NULL,               -- 'succeeded'|'still_unknown'|'duplicate_sent'
  detail_json TEXT,
  observed_at TIMESTAMP NOT NULL
);
```

The ledger — not `PermissionNotice`, not chat text — is the source of truth for what
happened. `PermissionNotice`/`PermissionState` continue to serve UI persistence for the
approval card, but the receipt lives here.

## 5. Gmail send execution and reconciliation

### 5.1 Execution sequence (per approved send)

1. `drafts.create` (if the draft was not already created by an earlier approved step) with
   the RFC822 message carrying a custom header `X-Argus-Op-Id: <op_id>` **and** a unique
   token in the body footer: `argus-op-<op_id>`.
2. `drafts.send` with the draft id. Client timeout set well below the tool-call timeout so
   Argus, not the transport, decides when a result is "not observed".
3. On success: store the returned `Message` (id, threadId, labelIds containing `SENT`) as
   the receipt; status → `succeeded`.

### 5.2 Reconciliation for timeouts / ambiguous outcomes

Gmail has no idempotency key, so a retry after an unknown outcome risks a duplicate send.
Instead of blind retry, resolve with postconditions:

1. **Draft-gone check.** `drafts.get(draftId)`: a successfully sent draft is deleted by
   Gmail. Draft gone ⇒ the send was processed ⇒ verify further with (2), else status may
   return to a safe retry (the send did not happen).
2. **Sent-search check.** `messages.list` with `q="argus-op-<op_id>"` restricted to the
   account: if a message with the unique body token exists (label `SENT`), the send
   succeeded exactly once — store receipt, status → `succeeded`.
3. **Duplicate detection.** If more than one message matches the op token (possible only if
   an earlier ambiguous attempt actually landed and a later retry also landed), record
   `duplicate_sent` in `action_reconciliations` and surface it to the user honestly.
4. Only if (1) proves the send never started may a retry reuse the **same op_id and same
   token** — a new attempt reuses the draft rather than creating a second one.

The op token in the body is the practical idempotency key Gmail does not offer; it makes
"did this exact send happen?" answerable with Gmail's own search.

## 6. Budgets and loop guards

Enforced in `agent_loop.go` / `send.go`, recorded in `agent_run_metrics`:

- **Model-round cap**: hard cap per turn, lowered from the 250 backstop to a configurable
  `maxModelRoundsPerTurn` (default 12) for interactive turns; background turns keep the
  backstop but gain the caps below.
- **Tool-call cap**: max tool calls per turn (default 25), independent of rounds.
- **Semantic no-progress cap**: replace signature-only stuck detection with a no-progress
  detector — N consecutive rounds (default 5) with no new successful tool result, no new
  content, and no state change ⇒ terminate with an honest wrap-up ("I could not find a way
  to do X; here is what exists"), never a fabricated outcome. Varied discovery queries no
  longer evade it because the input is the *outcome*, not the signature.
- **Cost/token cap**: per-turn and per-run token/cost ceilings; exceeded ⇒ controlled stop,
  recorded, surfaced on the run card.
- **Retry budget**: per-tool and per-turn retry ceilings; a tool failing with the same
  error (e.g. `429 daily_limit_reached`) terminates the turn for that tool immediately
  after one retry — 27 discovery calls against a closed door must be impossible.
- **Mandatory final-verification round**: before the model may state an action outcome in
  prose, the loop injects the ledger state for that turn's actions into context
  ("action email.send: succeeded, message id …"). The deterministic final-response rule is:
  the closing message's claims about actions must match ledger status; unmatched success
  wording is rewritten by the wrapper, not trusted from the model.

## 7. Approval and UI wiring

- New permission family `email.send` in `Services/Permission/service.go` policy set
  (modes: always ask / remember for session / allow). Dispatch of the typed
  `email.send` tool pauses the loop on `permission:requested` exactly as existing
  blocking approvals do; the access tier no longer substitutes for this approval.
- Events (extend `Services/Shared/events.go`):
  `action:requested`, `action:status` (status transitions + receipt summary).
- Frontend: wire a card into `Chat.tsx` handling `action:*` events. Reuse the existing
  `EmailDraftCard.tsx` states (`pending | sending | sent | denied`) extended with
  `failed | unknown`; `unknown` renders the reconciliation status live. Follow the
  `PermissionCard` + `permission/hooks.ts` request/resolve event pattern.
- `Protocol/canonical.go`: add an `action-status` block type carrying
  `{actionId, kind, status, receiptSummary}` so transcripts record authoritative state
  alongside the model's prose.

## 8. Catalog/skill truthfulness (immediate fix, independent of rollout)

- `Services/Skills/data/gmail/SKILL.md`: remove `gmail__send_message`; document the actual
  23 tools and state explicitly that sending is performed by Argus' own `email.send`
  action after approval.
- `Services/Integrations/data/catalog.json`: change "Draft and send emails" to match
  reality ("Draft emails; sending requires explicit approval via Argus send action").

## 9. Rollout

1. **Stage 0 (no code risk):** catalog/SKILL truth fixes; add no-progress detector
   (5 consecutive all-failure rounds), per-turn tool-call cap (25), and the quota
   retry rule (one retry, then stop) to both the interactive loop and
   `RunAgentLoop`. `maxToolRounds` stays 250 as the last-resort backstop — it was
   previously lowered to 12 and cut real multi-step tasks mid-plan, and the new
   call/no-progress budgets bound the incident pattern directly. **Implemented.**
2. **Stage 1:** `migration23` ledger tables + `email.send` typed tool + adapter
   (dry-run mode: everything up to the real `drafts.send`, then stop and report
   `requested`→`denied` by default) with tests.
3. **Stage 2:** approval card + events + `action-status` block; enable real sends behind
   the `email.send` permission family, default "always ask".
4. **Stage 3:** reconciliation worker for `unknown` actions; per-turn cost caps; metrics
   dashboards on `agent_run_metrics` (rounds/calls/cost/retries per run).

## 10. Tests

- **Adapter unit tests** (httptest Gmail fake): success receipt; 429 mapping; timeout →
  `unknown` + reconcile path; draft-gone + sent-search reconciliation; duplicate
  detection; op-token presence in the drafted RFC822 body/header.
- **Loop tests**: no-progress detector fires on varied-query spinning (replay the incident
  pattern: discovery-only rounds must stop ≤ N); success-wording rewrite when no receipt;
  budget exhaustion produces honest wrap-up.
- **Gateway tests**: `email.send` dispatch blocked without permission approval even at
  `full` tier (extends `mcp_gateway_test.go` patterns).
- **Permission tests**: new family modes; deny path yields `denied` and no provider call.
- **UI tests**: `EmailDraftCard` renders all six states from `action:*` events;
  reconciliation transition `unknown → succeeded` re-renders.
- **Migration test**: `migration23` up/roundtrip.

## 11. Observability

- `agent_run_metrics` gains per-run: tool calls by tool name, rounds, retries, tokens, cost,
  actions by final status. Alert-worthy: any turn ending with success wording but no
  receipt (must be structurally impossible post-Stage 2; kept as an invariant check),
  repeated `unknown` sends, duplicate-detection events.

## 12. References

- Gmail REST: users.drafts.send — https://developers.google.com/workspace/gmail/api/reference/rest/v1/users.drafts/send
- Gmail REST: users.messages.send — https://developers.google.com/workspace/gmail/api/reference/rest/v1/users.messages.send
- Gmail MCP server tools — https://developers.google.com/workspace/gmail/api/reference/mcp
- MCP tool specification (annotations) — https://modelcontextprotocol.io/specification/2025-06-18/server/tools
- OWASP LLM06: Excessive Agency — https://genai.owasp.org/llmrisk/llm062025-excessive-agency/
- OpenAI Agents SDK: running agents (guardrails/limits) — https://openai.github.io/openai-agents-python/running_agents/
- Anthropic: Building effective agents — https://www.anthropic.com/research/building-effective-agents
- LangGraph recursion limits — https://docs.langchain.com/oss/python/langgraph/graph-api#recursion-limit
- Local evidence: `~/.config/argus/argus.db` (read-only reconstruction),
  `Services/Chat/agent_loop.go`, `Services/Chat/run_budget.go`, `Services/Chat/send.go`,
  `Services/Chat/mcp_gateway.go`, `Services/Chat/tool_mcp.go`, `Services/Permission/service.go`,
  `Services/Shared/events.go`, `Storage/mcp_connections.go`, `Storage/migrations.go`,
  `Storage/agent_run_metrics.go`, `Protocol/canonical.go`,
  `frontend/src/pages/Chat.tsx`, `frontend/src/components/chat/EmailDraftCard.tsx`,
  `frontend/src/components/chat/PermissionCard.tsx`,
  `Services/Skills/data/gmail/SKILL.md`, `Services/Integrations/data/catalog.json`.
