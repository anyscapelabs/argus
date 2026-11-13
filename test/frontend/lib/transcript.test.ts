import { describe, expect, it } from 'vitest'
import { weaveTranscript } from '../../../frontend/src/lib/transcript'

// Fixed timestamps: 1 Jan 2026, spaced a day/moments apart.
const T1 = Date.UTC(2026, 0, 1, 10, 0, 0)
const T2 = Date.UTC(2026, 0, 2, 11, 0, 0)
const T3 = Date.UTC(2026, 0, 2, 11, 30, 0)

const msg = (id: string, role: string, ts: number, status = 'complete') => ({
  id,
  role,
  text: `text of ${id}`,
  status,
  created_at: ts,
})

const base = (overrides: object = {}) => ({
  messages: [],
  noticeLines: [],
  artifacts: [],
  takeover: null,
  mcpConnect: null,
  inFlightTs: 0,
  ...overrides,
})

describe('weaveTranscript', () => {
  it('weaves an empty conversation into no items', () => {
    expect(weaveTranscript(base())).toEqual([])
  })

  it('orders messages newest-in-first by position, not by time', () => {
    const out = weaveTranscript(
      base({ messages: [msg('m1', 'user', T1), msg('m2', 'assistant', T2)] }),
    )
    const msgs = out.filter((i) => i.kind === 'msg')
    expect(msgs.map((i) => i.id)).toEqual(['m1', 'm2'])
  })

  it('places a chip before the first message and on day change', () => {
    const out = weaveTranscript(
      base({ messages: [msg('m1', 'user', T1), msg('m2', 'assistant', T2)] }),
    )
    const kinds = out.map((i) => i.kind)
    expect(kinds[0]).toBe('chip')
    // Day changed T1 → T2: two chips total.
    expect(out.filter((i) => i.kind === 'chip')).toHaveLength(2)
  })

  it('does not place a second chip within the same day', () => {
    const out = weaveTranscript(
      base({ messages: [msg('m1', 'user', T2), msg('m2', 'assistant', T3)] }),
    )
    expect(out.filter((i) => i.kind === 'chip')).toHaveLength(1)
  })

  it('skips streaming assistant rows but keeps the user question', () => {
    const out = weaveTranscript(
      base({ messages: [msg('m1', 'user', T1), msg('m2', 'assistant', T2, 'streaming')] }),
    )
    expect(out.filter((i) => i.kind === 'msg').map((i) => i.id)).toEqual(['m1'])
  })

  it('places a notice between the messages it sits between in time', () => {
    const out = weaveTranscript(
      base({
        messages: [msg('m1', 'user', T1), msg('m2', 'assistant', T3)],
        noticeLines: [{ id: 'n1', kind: 'permission', created_at: T2, text: 'hi' }],
      }),
    )
    const kinds = out.map((i) => i.kind)
    const noticeIdx = kinds.indexOf('notice')
    const msgIdxs = out
      .map((i, ix) => (i.kind === 'msg' ? ix : -1))
      .filter((i) => i >= 0)
    expect(noticeIdx).toBeGreaterThan(msgIdxs[0])
    expect(noticeIdx).toBeLessThan(msgIdxs[1])
  })

  it('places an artifact card where the document was written', () => {
    const out = weaveTranscript(
      base({
        messages: [msg('m1', 'user', T1), msg('m2', 'assistant', T3)],
        artifacts: [{ id: 'a1', created_at: T2, title: 'Report', words: 42, version: 1 }],
      }),
    )
    const cardIdx = out.findIndex((i) => i.kind === 'artifactCard')
    const msgIdxs = out
      .map((i, ix) => (i.kind === 'msg' ? ix : -1))
      .filter((i) => i >= 0)
    expect(cardIdx).toBeGreaterThan(msgIdxs[0])
    expect(cardIdx).toBeLessThan(msgIdxs[1])
  })

  it('includes the takeover card when one is live', () => {
    const out = weaveTranscript(
      base({
        messages: [msg('m1', 'user', T1)],
        takeover: { request_id: 'r1', agent_id: 'default', state: 'requested', created_at: T2 },
      }),
    )
    expect(out.some((i) => i.kind === 'takeoverCard')).toBe(true)
  })

  it('places the connector card at its creation point', () => {
    const out = weaveTranscript(
      base({
        messages: [msg('m1', 'user', T3)],
        mcpConnect: { request_id: 'r9', server_id: 'gmail', server_name: 'Gmail', created_at: T2 },
      }),
    )
    const kinds = out.map((i) => i.kind)
    expect(kinds.indexOf('mcpCard')).toBeLessThan(kinds.indexOf('msg'))
  })

  it('holds connector notices raised during the turn in flight', () => {
    const notice = { id: 'n5', kind: 'mcp', created_at: T3, text: 'connected' }
    const out = weaveTranscript(
      base({
        messages: [msg('m1', 'user', T1), msg('m2', 'assistant', T3)],
        noticeLines: [notice],
        inFlightTs: T2,
      }),
    )
    expect(out.some((i) => i.kind === 'notice')).toBe(false)
  })

  it('places a voice message note where the call happened', () => {
    const out = weaveTranscript(
      base({
        messages: [msg('m1', 'user', T1), msg('m2', 'assistant', T3)],
        voiceNotes: [{ id: 'v1', created_at: T2, seconds: 152 }],
      }),
    )
    const voice = out.find((i) => i.kind === 'voiceMsg')
    expect(voice).toBeDefined()
    if (voice?.kind === 'voiceMsg') expect(voice.seconds).toBe(152)
    const idx = out.findIndex((i) => i.kind === 'voiceMsg')
    const msgIdxs = out
      .map((i, ix) => (i.kind === 'msg' ? ix : -1))
      .filter((i) => i >= 0)
    expect(idx).toBeGreaterThan(msgIdxs[0])
    expect(idx).toBeLessThan(msgIdxs[1])
  })

  it('weaves a persisted voice record into a transcript card with turns', () => {
    const record = JSON.stringify({
      seconds: 75,
      turns: [
        { role: 'user', text: 'what is on my calendar?' },
        { role: 'model', text: 'You have a call at noon.' },
      ],
    })
    const voiceRow = { ...msg('m9', 'user', T2), kind: 'voice', text: record }
    const out = weaveTranscript(base({ messages: [msg('m1', 'user', T1), voiceRow] }))
    const voice = out.find((i) => i.kind === 'voiceMsg')
    expect(voice).toBeDefined()
    if (voice?.kind === 'voiceMsg') {
      expect(voice.seconds).toBe(75)
      expect(voice.turns).toHaveLength(2)
      expect(voice.turns?.[1].role).toBe('model')
    }
    // The JSON record must never surface as a plain chat message.
    expect(out.some((i) => i.kind === 'msg' && i.msg.id === 'm9')).toBe(false)
  })

  it('shows ask_user immediately while the run is still in flight', () => {
    const out = weaveTranscript(
      base({
        messages: [msg('m1', 'user', T1), msg('m2', 'assistant', T3, 'streaming')],
        inFlightTs: T2,
        askUser: {
          request_id: 'ask-1', agent_id: 'default', run_id: 'run-1',
          question: 'Which environment?', choices: ['Development', 'Production'], created_at: T3,
        },
      }),
    )
    expect(out.some((item) => item.kind === 'askUserCard')).toBe(true)
  })

  it('renders sanitized file writes as cards instead of messages', () => {
    const fileRow = {
      ...msg('file-1', 'assistant', T2),
      kind: 'file_write',
      text: '',
      file_write: { path: '/tmp/app.ts', action: 'changed', bytes: 120, lines: 8 },
    }
    const out = weaveTranscript(base({ messages: [fileRow] }))
    const card = out.find((item) => item.kind === 'fileWriteCard')
    expect(card).toBeDefined()
    if (card?.kind === 'fileWriteCard') expect(card.write.path).toBe('/tmp/app.ts')
    expect(out.some((item) => item.kind === 'msg' && item.id === 'file-1')).toBe(false)
  })
})
