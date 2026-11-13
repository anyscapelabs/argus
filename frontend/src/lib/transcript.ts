// The transcript weave: turns stored messages into the ordered feed the chat
// draws, interleaving time chips, notices, artifact cards, the takeover card
// and the connector card between the rows they happened between. Pure logic —
// no React, no events — so the placement rules can change without touching the
// component that renders the result.

import { HOUR_MS, dayChipLabel, dayKey, msgStamp } from './time'
import type { ArtifactView } from '../domains/artifacts/hooks'
import type { TakeoverRequestView } from '../domains/browser/api'
import type { MCPConnectRequestView } from '../domains/mcp/api'
import type { VoiceTurn } from '../../bindings/github.com/anyscapelabs/argus/Services/Shared/models.js'
import type { AskUserRequestView } from '../domains/askUser/api'
import type { FileWrite } from '../../bindings/github.com/anyscapelabs/argus/Protocol/models.js'

export type TranscriptItem =
  | { kind: 'chip'; id: string; label: string }
  | { kind: 'msg'; id: string; msg: any }
  | { kind: 'notice'; id: string; notice: any }
  | { kind: 'artifactCard'; id: string; artifact: ArtifactView }
  | { kind: 'takeoverCard'; id: string }
  | { kind: 'mcpCard'; id: string }
  | { kind: 'voiceMsg'; id: string; seconds: number; turns?: VoiceTurn[] }
  | { kind: 'askUserCard'; id: string }
  | { kind: 'fileWriteCard'; id: string; write: FileWrite }

// VoiceNote is a finished voice call recorded into the transcript: when it
// happened and how long it lasted. It renders as a notifier under the chat the
// way a permission notice does.
export type VoiceNote = { id: string; created_at: number; seconds: number }

// parseVoiceRecord reads the JSON a persisted 'voice' message row carries.
// Anything unreadable degrades to a bare card with no transcript to open.
function parseVoiceRecord(text: string): { seconds: number; turns: VoiceTurn[] } {
  try {
    const rec = JSON.parse(text)
    if (rec && Array.isArray(rec.turns)) {
      return { seconds: typeof rec.seconds === 'number' ? rec.seconds : 0, turns: rec.turns }
    }
  } catch {
    // fall through to the empty record
  }
  return { seconds: 0, turns: [] }
}

// Time chips woven into the transcript: a chip opens the conversation and
// every new local day, and repeats the clock whenever an hour or more has
// passed since the previous message. Every chip names the day — "Today at
// 9:12am", "Yesterday at 6:03pm", "Saturday at 11:58pm", "Oct 25 at 8:04am"
// — so returning after a night reads the timeline instead of guessing it.
// Notices ride the same weave: each one lands between the messages it sits
// between in time, where it happened, not in a pile at the bottom. The connect
// card rides it too, so the card keeps the place the sign-in was asked for
// while every newer message stacks up beneath it. And so does an artifact
// card: a document the agent wrote belongs where the writing happened, which
// the table already knows down to the millisecond.
export function weaveTranscript({
  messages,
  noticeLines,
  artifacts,
  takeover,
  mcpConnect,
  askUser,
  inFlightTs,
  voiceNotes,
}: {
  messages: any[]
  noticeLines: any[]
  artifacts: ArtifactView[]
  takeover: TakeoverRequestView | null
  mcpConnect: MCPConnectRequestView | null
  askUser?: AskUserRequestView | null
  inFlightTs: number
  voiceNotes?: VoiceNote[]
}): TranscriptItem[] {
  type Extra = {
    id: string
    ts: number
    notice?: any
    artifact?: ArtifactView
    takeover?: TakeoverRequestView
    voice?: VoiceNote
    askUser?: AskUserRequestView
  }
  const items: TranscriptItem[] = []
  let prevTs = 0
  let ni = 0
  const pushExtra = (e: Extra) => {
    if (e.notice) items.push({ kind: 'notice', id: e.id, notice: e.notice })
    else if (e.takeover) items.push({ kind: 'takeoverCard', id: e.id })
    else if (e.artifact) items.push({ kind: 'artifactCard', id: e.id, artifact: e.artifact })
    else if (e.voice) items.push({ kind: 'voiceMsg', id: e.id, seconds: e.voice.seconds })
    else if (e.askUser) items.push({ kind: 'askUserCard', id: e.id })
  }
  // One boundary for the whole weave: when a message *started*. A card or a
  // notice raised in the middle of a turn belongs under that turn's bubble, and
  // measuring them against the reply's finished time is what used to lift them
  // above the very message that produced them.
  const held = (ts: number) => inFlightTs > 0 && ts >= inFlightTs
  // The oldest message on screen. A document written before that window has
  // its list entry in the panel; stacking its card above the first thing the
  // user can read would put the newest conversation out of sight.
  let windowStart = 0
  for (const m of messages) {
    const s = m.created_at || 0
    if (s && (!windowStart || s < windowStart)) windowStart = s
  }
  // Notices and artifact cards share one merged timeline, sorted before the
  // walk below — that is what keeps two things that happened between the same
  // pair of messages in the order they actually happened.
  const extras: Extra[] = []
  // Only the connector's own rows wait. A terminal permission decision is the
  // answer to a click that already happened, so it belongs on screen the
  // instant it lands, and the weave below still drops it under the ask.
  for (const n of noticeLines) {
    if (n.kind === 'mcp' && held(n.created_at)) continue
    extras.push({ id: `notice-${n.id}`, ts: n.created_at, notice: n })
  }
  // A finished voice call is a note in the conversation: it happened at a moment
  // and belongs between the messages around it, exactly like a notice.
  for (const v of voiceNotes ?? []) {
    extras.push({ id: `voice-${v.id}`, ts: v.created_at, voice: v })
  }
  if (askUser) {
    extras.push({
      id: `ask-user-${askUser.request_id}`,
      ts: askUser.created_at || Number.MAX_SAFE_INTEGER,
      askUser,
    })
  }
  // A document is written in the middle of a conversation, so its card needs
  // one to belong to. With no messages on screen there is nowhere to put it,
  // and the panel already lists every document.
  if (messages.length > 0) {
    for (const a of artifacts) {
      const ts = a.created_at || 0
      if (held(ts) || (windowStart && ts < windowStart)) continue
      extras.push({ id: `artifact-${a.id}`, ts, artifact: a })
    }
  }
  // The handover card keeps the place the agent raised it, and it waits for
  // the reply in flight the same way a connector card does: while a turn is
  // still streaming the reply is not drawn yet, so a card placed now would
  // end up sitting above the message that asked for it.
  if (takeover) {
    const ts = takeover.created_at || 0
    if (!held(ts)) {
      extras.push({
        id: `takeover-${takeover.request_id}`,
        ts: ts || Number.MAX_SAFE_INTEGER,
        takeover,
      })
    }
  }
  extras.sort((x, y) => (x.ts || 0) - (y.ts || 0))
  // A card whose payload carried no timestamp has nothing to hold, so it stays
  // at the end the way it always did. One raised inside the turn in flight waits
  // with that reply, so the agent presents its message before the card appears.
  const cardTs = mcpConnect ? mcpConnect.created_at || 0 : 0
  let cardPlaced = !mcpConnect || held(cardTs)
  const placeCard = () => {
    if (cardPlaced || !mcpConnect) return
    items.push({
      kind: 'mcpCard',
      id: `mcp-card-${mcpConnect.request_id || mcpConnect.server_id}`,
    })
    cardPlaced = true
  }
  // The card goes in front of the first item that started after it did. For a
  // message the test stays strict, so a reply that opened before the card keeps
  // its slot even when it lands late: the card never jumps above the ask it
  // answers. Notices pass cardOnTie because a restored card carries its own
  // notice's stamp, and the ask still has to read above what it reports.
  const placeBefore = (start: number, cardOnTie = false) => {
    if (cardTs <= 0 || cardPlaced) return
    if (cardTs < start || (cardOnTie && cardTs === start)) placeCard()
  }
  for (const m of messages) {
    if (m.role !== 'user' && m.status === 'streaming') continue
    const ts = msgStamp(m)
    const start = m.created_at || ts
    if (m.kind === 'voice') {
      // A persisted voice call: drawn as a transcript card exactly where the
      // call happened, carrying its own record of turns and duration.
      const rec = parseVoiceRecord(m.text || '')
      if (ts && (!prevTs || dayKey(ts) !== dayKey(prevTs) || ts - prevTs >= HOUR_MS)) {
        items.push({ kind: 'chip', id: `chip-${m.id}`, label: dayChipLabel(ts) })
      }
      items.push({ kind: 'voiceMsg', id: m.id, seconds: rec.seconds, turns: rec.turns })
      if (ts) prevTs = ts
      continue
    }
    if (m.kind === 'file_write' && m.file_write?.path) {
      if (ts && (!prevTs || dayKey(ts) !== dayKey(prevTs) || ts - prevTs >= HOUR_MS)) {
        items.push({ kind: 'chip', id: `chip-${m.id}`, label: dayChipLabel(ts) })
      }
      items.push({ kind: 'fileWriteCard', id: m.id, write: m.file_write })
      if (ts) prevTs = ts
      continue
    }
    while (ni < extras.length && (!start || extras[ni].ts < start)) {
      placeBefore(extras[ni].ts, true)
      pushExtra(extras[ni])
      ni++
    }
    if (ts && (!prevTs || dayKey(ts) !== dayKey(prevTs) || ts - prevTs >= HOUR_MS)) {
      placeBefore(start)
      items.push({ kind: 'chip', id: `chip-${m.id}`, label: dayChipLabel(ts) })
    }
    placeBefore(start)
    // A row written in the same instant as this message reads best after it:
    // the ask and the answer keep their order.
    while (ni < extras.length && extras[ni].ts === start) {
      pushExtra(extras[ni])
      ni++
    }
    items.push({ kind: 'msg', id: m.id, msg: m })
    if (ts) prevTs = ts
  }
  while (ni < extras.length) {
    placeBefore(extras[ni].ts, true)
    pushExtra(extras[ni])
    ni++
  }
  // Nothing came after it, so this card is the newest thing in the room.
  placeCard()
  return items
}
