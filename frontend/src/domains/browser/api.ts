import * as ChatService from '../../../bindings/github.com/anyscapelabs/argus/Services/Chat/service.js'
import type { ActionResult } from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models.js'

// The browser handover, as the UI knows it. The agent asks; the card is the
// user's chance to drive Chrome by hand for the one step the agent must not do
// alone — a CAPTCHA, a sign-in, a card number. Everything about the page itself
// stays in Services/Browser; this file is only the two calls the card makes.

export type TakeoverState = 'requested' | 'active' | 'done' | 'declined'

// The three buttons. "takeover" starts the user driving, "done" hands the page
// back and resumes the agent, "decline" ends it without their hands.
export type TakeoverDecision = 'takeover' | 'done' | 'decline'

// Mirrors Shared.TakeoverRequestView.
export type TakeoverRequestView = {
  request_id: string
  agent_id: string
  agent_name?: string
  // Why the agent asked: the literal "captcha" when a human check was detected
  // on the page, otherwise the agent's own sentence.
  reason?: string
  // A PNG data URL of the agent's viewport. A data URL because the webview
  // cannot read the OS cache directory off disk.
  screenshot?: string
  host?: string
  page_title?: string
  state: TakeoverState
  // Unix milliseconds, the instant the card joined the conversation, so the
  // transcript can anchor it where the ask happened instead of drifting it to
  // the bottom as new messages arrive.
  created_at: number
}

// The binding types `state` as a string because Go owns the vocabulary. These
// are the states takeover.go raises. One the UI does not recognise stays an ask
// rather than going quiet: the agent blocks every browser call until a handover
// ends, so a card the user never saw would strand the run, while a card that
// turns out to be settled only costs one click.
function asTakeoverState(value: string): TakeoverState {
  return value === 'active' || value === 'done' || value === 'declined' ? value : 'requested'
}
export { asTakeoverState }

// The browser:takeover:status payload: a settled state, without the screenshot.
export type TakeoverStatus = {
  request_id: string
  agent_id: string
  state: TakeoverState
  error?: string
  host?: string
  created_at?: number
}

// The live card for this agent, if any. A handover that already ended is not
// restored — the notice row in the conversation keeps that story — so an empty
// list after a remount is the normal answer, not a lost card.
export async function pendingTakeovers(agentID: string): Promise<TakeoverRequestView[]> {
  const res = await ChatService.PendingTakeover(agentID)
  if (res?.error) throw new Error(res.error.message)
  return (res.takeovers ?? []).map((t) => ({ ...t, state: asTakeoverState(t.state) }))
}

export async function respondTakeover(
  requestID: string,
  decision: TakeoverDecision,
): Promise<ActionResult> {
  const res = await ChatService.RespondTakeover(requestID, decision)
  if (res?.error) throw new Error(res.error.message)
  return res
}
