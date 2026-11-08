# Chat pipeline

One user message travels through six stages. Each stage owns one kind of decision
and must not make another stage's. When something breaks, find the stage that owns
the symptom and fix it there; if the fix needs a second stage to change, the
boundary is wrong and should be moved first.

| # | Stage | Owns | Lives in | Must not |
|---|-------|------|----------|----------|
| 1 | Prompt | What we ask the model to do (advisory) | `Services/Chat/prompt.go` | be the only defence for a rule the UI depends on |
| 2 | Transport | Provider wire format to canonical events/blocks | `Protocol/*`, `Router/` | know about tools, artifacts or the UI |
| 3 | Agent loop | Rounds: call model, run tools, repeat | `Services/Chat/send.go` (`runTurn`) | decide what text is shown |
| 4 | Tools | Side effects and the text the model gets back | `Services/Chat/tools.go`, `artifact_tool.go` | write chat rows |
| 5 | Persistence | Rows: transcript blocks + display text | `Storage/messages.go`, `persistRound` | rewrite meaning; only the pipeline's own filters run before it |
| 6 | Presentation | What the bubble may show | `Services/Chat/Present` (pure), `GetChat` | touch the database or the provider |

## The artifact contract

A document has exactly one delivery surface: the **card**, drawn from the
`artifacts` table. The chat bubble may talk *about* the document but must never
contain it.

Because stage 1 is only advice, the contract is enforced in code, in stage 3/6:

1. `runTurn` notes when a round calls `write_artifact`.
2. Before a round row or the final reply is persisted, `withoutArtifactEcho`
   loads the bodies saved or extended since the turn started and
   `Present.StripArtifactEcho` removes paragraphs that copy them.
3. If that empties the final reply, one fallback sentence is stored so the turn
   does not end on a card with no words.

Failure paths are handled where they start, not by the filter:

- Truncated tool arguments: `invalidCallMessage` tells the model to retry in
  parts with `append=true`.
- Validator rejection: the tool result says the document was NOT saved and not
  to paste it. The filter deliberately compares only against **saved** bodies, so
  a rejected document pasted as a last resort is never hidden (it would be the
  user's only copy).

## Rules for changing this code

- Presentation filters go in `Services/Chat/Present`: pure functions on strings,
  one file per rule, a test per behaviour, no imports from the app.
- A tool handler lives in its own file and is reached from `runTool` by a single
  dispatch line. `runTool` stays a router.
- Rewrites of stored text happen once, at persistence, so history replay
  (`buildHistory`) never teaches the model that pasting documents is normal.
- Never fix a UI symptom by adding a special case to the frontend if the
  backend can make the bad state impossible.

## Known follow-ups (not in this change)

- `send.go` (1.4k lines) mixes the loop, history building/repair and cost
  accounting. Split into `loop.go`, `history.go`, `cost.go`.
- `runTool` is still an if-chain; move to a `map[string]toolHandler` registry
  one tool at a time, starting with the ones that already have their own file.
- `Chat.tsx` builds the transcript in a 130-line `useMemo`. Extract it to a pure
  `buildTranscript()` in `domains/chat/` with unit tests.
- `StripLeadingTitle` runs in three places (live filter, history, display); fold
  it into `Present` behind the same entry point.
- Rows stored before this change can still contain a pasted document; a one-off
  backfill using the same filter can clean them.