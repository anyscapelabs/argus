import { useEffect, useMemo, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Events } from '@wailsio/runtime'
import { useAgentProfile } from '../domains/models/hooks'
import { useChatMessages, useSendMessage } from '../domains/chat/hooks'
import { respondPermission, noticeText, type PermissionRequestView, type PermissionDecision } from '../domains/permission/api'
import { respondMCPConnect, pendingMCPConnects, type MCPConnectRequestView, type MCPConnectState } from '../domains/mcp/api'
import { asTakeoverState, pendingTakeovers, respondTakeover, type TakeoverDecision, type TakeoverRequestView, type TakeoverState, type TakeoverStatus } from '../domains/browser/api'
import { useNoticeLines } from '../domains/permission/hooks'
import { useArtifacts, type ArtifactView } from '../domains/artifacts/hooks'
import { motion, AnimatePresence } from 'framer-motion'
import { FiChevronRight, FiGlobe, FiLink, FiTerminal } from 'react-icons/fi'
import Toolbar from '../components/layout/Toolbar'
import ChatInput from '../components/chat/ChatInput'
import AgentBubble from '../components/chat/AgentBubble'
import UserBubble from '../components/chat/UserBubble'
import ThinkingIndicator from '../components/chat/ThinkingIndicator'
import Spinner from '../components/Spinner'
import { StatusLine } from '../components/chat/statusLine'
import PermissionCard, { type Permission } from '../components/chat/PermissionCard'
import MCPConnectCard from '../components/mcp/MCPConnectCard'
import TakeoverCard from '../components/browser/TakeoverCard'
import Notifier from '../components/chat/Notifier'
import RightPanel from '../components/layout/RightPanel'
import ArtifactCard from '../components/chat/ArtifactCard'
import { HOUR_MS, dayChipLabel, dayKey, msgStamp } from '../lib/time'

const messagesKey = ['chat', 'default', 'history']
const usageKey = ['chat', 'default', 'usage']
const noticesKey = ['chat', 'default', 'notices']

type PendingSend = { id: number; text: string; modelRef: string | null }

export default function Chat({
  pendingSend,
  onPendingConsumed,
  onOpenArtifact,
}: {
  pendingSend?: PendingSend | null
  onPendingConsumed?: () => void
  // Opens one document by id. A card is a window onto a row of the artifacts
  // table, so it names the row it came from rather than asking the app to
  // guess which document the user means.
  onOpenArtifact?: (artifactID: string) => void
}) {
  const [rightOpen, setRightOpen] = useState(false)
  const [permission, setPermission] = useState<Permission | null>(null)
  const [mcpConnect, setMcpConnect] = useState<MCPConnectRequestView | null>(null)
  const [mcpState, setMcpState] = useState<MCPConnectState>('requested')
  const [mcpError, setMcpError] = useState<string>()
  // The browser handover, one live card per agent like the connector's. The
  // backend holds it in memory and hands it back on mount, so a reload during a
  // handover restores the card instead of stranding the user mid-page.
  const [takeover, setTakeover] = useState<TakeoverRequestView | null>(null)
  const [takeoverState, setTakeoverState] = useState<TakeoverState>('requested')
  const [takeoverError, setTakeoverError] = useState<string>()
  const { data: agentProfile } = useAgentProfile('default')
  const qc = useQueryClient()
  const {
    data: messages = [],
    error: messagesError,
    isLoading,
    hasNextPage,
    isFetchingNextPage,
    fetchNextPage,
  } = useChatMessages('default')
  const send = useSendMessage('default')
  // Notices come from the database, so every line the notifier is told about
  // outlives the moment it happened, including a full restart.
  const noticeLines = useNoticeLines('default')
  // The documents this agent has written. They come from the table, not from the
  // transcript, because a document outlives the turn that made it: a card woven
  // from tool output would have to be reconstructed from message text, and the
  // table already knows the title, the version and the moment it landed.
  const { data: artifacts = [] } = useArtifacts('default')
  const noticeKeyText = noticeLines.map((n) => noticeText(n)).join('|')
  const [errorNotice, setErrorNotice] = useState<string | null>(null)
  const pendingConsumedId = useRef<number | null>(null)
  // What the indicator shows, paced by StatusLine so it stays readable.
  const [status, setStatus] = useState<string | null>(null)
  const [runActive, setRunActive] = useState(false)
  const statusLineRef = useRef<StatusLine | null>(null)
  // Sites a web search just returned: the indicator cycles favicon plus
  // result title while the model works on the answer.
  const [searchSources, setSearchSources] = useState<{ host: string; title: string }[]>([])
  const [sourceIdx, setSourceIdx] = useState(0)
  // The site the agent is driving right now, from the browser tool's own events.
  // One entry, not a cycle: there is exactly one page under the cursor.
  const [browsing, setBrowsing] = useState<{ host: string; title: string } | null>(null)

  // Only a failure the frontend caused itself: the Go side could not be
  // reached, so there is no notice row for the conversation to keep.
  const showLocalError = (text: string, ms = 6000) => {
    setErrorNotice(text)
    setTimeout(() => setErrorNotice(null), ms)
  }

  const decide = (decision: PermissionDecision) => {
    if (!permission) return
    const requestID = permission.request_id
    setPermission(null)
    respondPermission(requestID, decision).catch((err) => {
      console.error('permission respond failed', err)
      showLocalError('Could not save that decision, try again')
    })
  }

  const decideMCP = (decision: 'connect' | 'deny' | 'cancel') => {
    if (!mcpConnect) return
    if (decision === 'deny') {
      setMcpConnect(null)
      setMcpState('requested')
      setMcpError(undefined)
    }
    respondMCPConnect(mcpConnect.request_id, decision).catch((err) => {
      setMcpState('failed')
      setMcpError(err.message)
    })
  }

  // The card never decides for itself: every button tells the backend, and the
  // backend answers with the state the card then shows. That is also what makes
  // a failed hand-over visible — the settled status arrives with its reason, and
  // the conversation keeps a notice row for it whether or not this window is
  // still open.
  const decideTakeover = (decision: TakeoverDecision) => {
    if (!takeover) return
    respondTakeover(takeover.request_id, decision).catch((err) => {
      setTakeoverError(err.message)
    })
  }

  useEffect(() => {
    pendingMCPConnects('default').then((l) => {
      const v = l && l[0]
      if (!v) return
      setMcpConnect(v)
      setMcpState(v.state || 'requested')
      setMcpError(undefined)
    })
    pendingTakeovers('default').then((l) => {
      const v = l && l[0]
      if (!v) return
      setTakeover(v)
      setTakeoverState(v.state)
      setTakeoverError(undefined)
    }).catch(() => {
      // A backend that is not up yet has no card to restore, and the events
      // will raise one the moment it does.
    })
    const line = new StatusLine(setStatus)
    statusLineRef.current = line
    const unsubs = [
      Events.On('permission:requested', (event: any) => {
        const payload = (event?.data ?? event) as PermissionRequestView
        if (!payload?.request_id) return
        setPermission(payload)
      }),
      Events.On('permission:resolved', (event: any) => {
        const payload = (event?.data ?? event) as { request_id?: string } | undefined
        qc.invalidateQueries({ queryKey: noticesKey })
        setPermission((cur) => (cur && payload?.request_id !== cur.request_id ? null : cur))
      }),
      Events.On('mcp:connect:status', (event: any) => {
        const payload = (event?.data ?? event) as any
        if (!payload?.request_id) return
        if (payload.state === 'requested') {
          setMcpConnect({
            request_id: payload.request_id,
            agent_id: payload.agent_id,
            server_id: payload.server_id,
            server_name: payload.server_name,
            description: payload.description,
            icon: payload.icon,
            created_at: payload.created_at || 0,
          })
        }
        setMcpState(payload.state)
        setMcpError(payload.error)
        if (payload.state === 'connected') {
          // Keep the card up: it is the only place that says this integration
          // is connected, and it must not vanish the moment the sign-in works.
          setMcpConnect((cur) => (
            cur && cur.server_id === payload.server_id
              ? cur
              : {
                request_id: payload.request_id,
                agent_id: payload.agent_id,
                server_id: payload.server_id,
                server_name: payload.server_name,
                description: payload.description,
                icon: payload.icon,
                created_at: payload.created_at || 0,
              }
          ))
        }
        if (payload.state === 'denied' || payload.state === 'expired') {
          setMcpConnect(null)
        }
        // Every state the backend settles on is already a notice row, so the
        // conversation picks it up from the database instead of a toast that
        // disappears after a few seconds.
        if (payload.state !== 'requested' && payload.state !== 'authorizing') {
          qc.invalidateQueries({ queryKey: noticesKey })
        }
      }),
      Events.On('browser:takeover:requested', (event: any) => {
        const payload = (event?.data ?? event) as TakeoverRequestView
        if (!payload?.request_id) return
        // The raise carries the page: screenshot, host and the reason the agent
        // gave, all of which the card needs before the user decides anything.
        setTakeover(payload)
        setTakeoverState(asTakeoverState(payload.state))
        setTakeoverError(undefined)
      }),
      Events.On('browser:takeover:status', (event: any) => {
        const payload = (event?.data ?? event) as TakeoverStatus
        if (!payload?.request_id) return
        const state = asTakeoverState(payload.state)
        // A state the UI does not know is treated as still asking, so the card
        // stays where the user can answer it instead of vanishing on them.
        if (state === 'requested') return
        if (state === 'active') {
          setTakeoverState('active')
          setTakeoverError(payload.error)
          return
        }
        // done | declined. The card's job ends here rather than staying up with
        // a settled pill, because the backend writes a notice row at the same
        // instant and the weave puts it exactly where the ask was: two rows
        // about one handover is one more than the user needs.
        setTakeover(null)
        setTakeoverState('requested')
        setTakeoverError(undefined)
        qc.invalidateQueries({ queryKey: noticesKey })
      }),
      Events.On('agent:activity', (event: any) => {
        const payload = (event?.data ?? event) as
          | {
              kind?: string
              command?: string
              tool?: string
              sources?: { host?: string; title?: string }[]
              page?: { host?: string; title?: string; action?: string; tab?: number }
            }
          | undefined
        if (!payload?.kind) return
        const isSearch = payload.tool === 'web_search'
        const isBrowser = payload.tool === 'browser'
        if (payload.kind === 'tool_started') {
          const cmd = (payload.command ?? '').trim()
          if (payload.tool === 'web_search') {
            // The query itself is noise here; the found sites speak instead.
            setSearchSources([])
            line.set('Searching the web', 'tool')
          } else if (isBrowser) {
            // The site is the interesting part and it is not known yet; the
            // tool_results event names it as soon as Chrome has answered.
            setBrowsing(null)
            line.set('Opening a page', 'tool')
          } else {
            const short = cmd.length > 60 ? cmd.slice(0, 60) + '…' : cmd
            line.set(
              cmd.length > 0 ? `Running ${short} in the terminal` : 'Running a command in the terminal',
              'tool',
            )
          }
        } else if (payload.kind === 'tool_results' && isSearch) {
          // Show the found sites (favicon + title, cycling) while the model
          // works on the answer, instead of dropping straight back to text.
          const sites = (payload.sources ?? []).filter(
            (s): s is { host: string; title: string } => !!s?.host && !!s?.title,
          )
          setSearchSources(sites)
          setSourceIdx(0)
        } else if (isBrowser && payload.kind === 'tool_results') {
          const page = payload.page
          if (page?.host) {
            setBrowsing({ host: page.host, title: page.title ?? '' })
            line.set(`Browsing ${page.host}`, 'tool')
          }
        } else if (isBrowser) {
          // A finished browser action leaves the page where it was; clearing the
          // line here would make the indicator blink between every step.
        } else {
          line.clear()
        }
      }),
      Events.On('chat:message', (event: any) => {
        setRunActive(true)
        line.set('Thinking…', 'label')
        qc.invalidateQueries({ queryKey: messagesKey })
        qc.invalidateQueries({ queryKey: usageKey })
      }),
      Events.On('model:stream', (event: any) => {
        const ev = (event?.data ?? event) as { event?: { type?: string; delta?: { kind?: string } } } | undefined
        const type = ev?.event?.type
        const deltaKind = ev?.event?.delta?.kind
        // First text token → switch indicator from Thinking… to Typing…
        if (type === 'block_delta' && deltaKind === 'text') {
          line.set('Typing…', 'label')
        }
      }),
      // chat:typing fires from the Send path on the first text token
      Events.On('chat:typing', () => {
        // Text is streaming: the source cycle's job is done.
        setSearchSources([])
        setBrowsing(null)
        line.set('Typing…', 'label')
      }),
      Events.On('chat:run_finished', () => {
        setSearchSources([])
        setBrowsing(null)
        line.reset()
        setRunActive(false)
        qc.invalidateQueries({ queryKey: messagesKey })
        qc.invalidateQueries({ queryKey: usageKey })
      }),
    ]
    return () => {
      unsubs.forEach((u) => u && u())
      line.dispose()
      if (statusLineRef.current === line) {
        statusLineRef.current = null
      }
    }
  }, [qc])

  // Rotate the web-search sites every couple seconds so each result gets a
  // beat on screen.
  useEffect(() => {
    if (searchSources.length < 2) return
    const t = setInterval(() => setSourceIdx((i) => (i + 1) % searchSources.length), 2600)
    return () => clearInterval(t)
  }, [searchSources])

  // Current chip for the indicator: favicon + title of the site shown now.
  const chip = useMemo(() => {
    if (searchSources.length > 0) {
      const s = searchSources[sourceIdx % searchSources.length]
      return { favicon: `https://icons.duckduckgo.com/ip3/${s.host}.ico`, title: s.title }
    }
    if (browsing) {
      return {
        favicon: `https://icons.duckduckgo.com/ip3/${browsing.host}.ico`,
        title: browsing.title || browsing.host,
      }
    }
    return null
  }, [searchSources, sourceIdx, browsing])

  const handleSend = (text: string, _modelRef: string | null) => {    if (send.isPending || runActive) return
    setRunActive(true)
    statusLineRef.current?.set('Thinking…', 'label')
    send.mutate(text, {
      onError: (err: any) => {
        console.error('send failed', err)
        setRunActive(false)
      },
    })
  }

  useEffect(() => {
    if (!pendingSend?.text) return
    if (pendingConsumedId.current === pendingSend.id) return
    if (send.isPending || runActive) return
    pendingConsumedId.current = pendingSend.id
    onPendingConsumed?.()
    setRunActive(true)
    statusLineRef.current?.set('Thinking…', 'label')
    send.mutate(pendingSend.text, {
      onError: (err: any) => {
        console.error('send failed', err)
        setRunActive(false)
      },
    })
  }, [pendingSend])

  // The turn in flight has a database row but is deliberately not drawn until it
  // finishes, so anything anchored inside that window waits with it: the card and
  // its notifier must land under the reply that produced them, never above it.
  // Gated on runActive because a row left 'streaming' by a crash would otherwise
  // hold the card back forever, and a reload has no live turn to wait for.
  const inFlightTs = useMemo(() => {
    if (!runActive) return 0
    let start = 0
    for (const m of messages) {
      if (m.role === 'user' || m.status !== 'streaming') continue
      const s = m.created_at || 0
      if (s && (!start || s < start)) start = s
    }
    return start
  }, [messages, runActive])

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
  const transcript = useMemo(() => {
    type Item =
      | { kind: 'chip'; id: string; label: string }
      | { kind: 'msg'; id: string; msg: any }
      | { kind: 'notice'; id: string; notice: any }
      | { kind: 'artifactCard'; id: string; artifact: ArtifactView }
      | { kind: 'takeoverCard'; id: string }
      | { kind: 'mcpCard'; id: string }
    type Extra = { id: string; ts: number; notice?: any; artifact?: ArtifactView; takeover?: TakeoverRequestView }
    const items: Item[] = []
    let prevTs = 0
    let ni = 0
    const pushExtra = (e: Extra) => {
      if (e.notice) items.push({ kind: 'notice', id: e.id, notice: e.notice })
      else if (e.takeover) items.push({ kind: 'takeoverCard', id: e.id })
      else if (e.artifact) items.push({ kind: 'artifactCard', id: e.id, artifact: e.artifact })
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
  }, [messages, noticeLines, artifacts, takeover, mcpConnect, inFlightTs])

  const scrollRef = useRef<HTMLDivElement>(null)
  const pinnedRef = useRef(true)
  // Where the viewport sat when an older page was requested. Prepending
  // history pushes every pixel down; without this anchor the reader of an old
  // message is thrown to the bottom of the new stack.
  const prependAnchor = useRef<{ height: number; top: number } | null>(null)
  const onScroll = () => {
    const el = scrollRef.current
    if (!el) return
    pinnedRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 120
    if (el.scrollTop < 160 && hasNextPage && !isFetchingNextPage) {
      prependAnchor.current = { height: el.scrollHeight, top: el.scrollTop }
      fetchNextPage()
    }
  }
  useEffect(() => {
    const el = scrollRef.current
    const anchor = prependAnchor.current
    if (!el || !anchor || isFetchingNextPage) return
    prependAnchor.current = null
    if (el.scrollHeight > anchor.height) {
      el.scrollTop = el.scrollHeight - anchor.height + anchor.top
    }
  }, [messages, isFetchingNextPage])
  // Follow the stream: keep the latest message visible while the user is
  // pinned near the bottom; scrolling up to read pauses the auto-follow.
  useEffect(() => {
    const el = scrollRef.current
    if (!el || !pinnedRef.current) return
    el.scrollTop = el.scrollHeight
  }, [messages, permission, noticeKeyText, errorNotice, status, mcpConnect?.request_id, takeover?.request_id, artifacts.length])

  return (
    <div className="relative flex h-dvh flex-1 min-h-0 overflow-hidden">
      <div className="flex flex-1 flex-col min-h-0">
        <Toolbar />
        <div className="relative flex flex-1 min-h-0 flex-col">
          <div className="absolute left-1/2 -top-10 z-10 -translate-x-1/2">
            <motion.button
              type="button"
              onClick={() => setRightOpen((v) => !v)}
              transition={{ type: 'spring', stiffness: 300, damping: 20 }}
              className="group flex cursor-pointer items-center gap-2 rounded-full bg-secondary px-3 py-1 transition-colors hover:bg-hover-secondary"
            >
              <img src="/argus-logo.svg" alt="Argus" className="h-10 w-10 rounded-full" />
              <span className="text-sm font-medium text-primary">{agentProfile?.agent_name || 'Argus'}</span>
              <FiChevronRight size={14} className="w-0 overflow-hidden opacity-0 transition-all duration-300 ease-out group-hover:w-4 group-hover:translate-x-1 group-hover:opacity-100" />
            </motion.button>
          </div>
          <div ref={scrollRef} onScroll={onScroll} className="flex-1 min-h-0 overflow-y-auto px-6 pt-6 pb-24">
            <div className="mx-auto flex max-w-3xl flex-col gap-3">
              {isLoading && <div className="flex flex-1 items-center justify-center py-12"><Spinner /></div>}
              {isFetchingNextPage && <div className="mt-1 text-center text-xs text-secondary">Loading older messages…</div>}
              {!isLoading && !hasNextPage && messages.length > 0 && (
                <div className="mt-1 text-center text-xs text-secondary">This is the start of the conversation</div>
              )}
              {messagesError && <div className="text-xs text-red-400">{String(messagesError)}</div>}
              {send.isError && (
                <div className="w-fit rounded-xl border border-red-400/40 bg-secondary px-2 py-1 text-xs text-red-400">
                  {String((send.error as any)?.message ?? send.error)}
                </div>
              )}
              {transcript.map((item) =>
                item.kind === 'chip' ? (
                  <div key={item.id} className="mx-auto mt-5 mb-1 w-fit rounded-md px-2 py-0.5 text-center text-xs text-secondary transition-colors hover:bg-secondary hover:text-primary first:mt-0">
                    {item.label}
                  </div>
                ) : item.kind === 'notice' ? (
                  <Notifier
                    key={item.id}
                    text={noticeText(item.notice)}
                    icon={
                      item.notice.kind === 'mcp' ? (
                        <div className="flex h-4 w-4 items-center justify-center rounded-md border border-secondary"><FiLink size={9} className="text-secondary" /></div>
                      ) : item.notice.kind === 'takeover' ? (
                        <div className="flex h-4 w-4 items-center justify-center rounded-md border border-secondary"><FiGlobe size={9} className="text-secondary" /></div>
                      ) : (
                        <div className="flex h-4 w-4 items-center justify-center rounded-md border border-secondary"><FiTerminal size={9} className="text-secondary" /></div>
                      )
                    }
                  />
                ) : item.kind === 'mcpCard' ? (
                  mcpConnect && (
                    <MCPConnectCard
                      key={item.id}
                      request={mcpConnect}
                      state={mcpState}
                      error={mcpError}
                      onConnect={() => decideMCP('connect')}
                      onDeny={() => decideMCP('deny')}
                      onCancel={() => decideMCP('cancel')}
                    />
                  )
                ) : item.kind === 'takeoverCard' ? (
                  takeover && (
                    <TakeoverCard
                      key={item.id}
                      request={takeover}
                      state={takeoverState}
                      error={takeoverError}
                      onTakeover={() => decideTakeover('takeover')}
                      onDone={() => decideTakeover('done')}
                      onDecline={() => decideTakeover('decline')}
                    />
                  )
                ) : item.kind === 'artifactCard' ? (
                  <div key={item.id} className="w-[450px]">
                    <ArtifactCard
                      title={item.artifact.title}
                      words={item.artifact.words}
                      version={item.artifact.version}
                      onClick={() => onOpenArtifact?.(item.artifact.id)}
                    />
                  </div>
                ) : item.msg.role === 'user' ? (
                  <UserBubble key={item.id} content={item.msg.text} />
                ) : (
                  <AgentBubble key={item.id} content={item.msg.text} status={item.msg.status} error={item.msg.error_message} />
                ),
              )}
              {permission && (
                <PermissionCard
                  permission={permission}
                  agentName={agentProfile?.agent_name}
                  onAllowAlways={() => decide('allow_always')}
                  onAllowOnce={() => decide('allow_once')}
                  onDeny={() => decide('deny')}
                />
              )}
              {errorNotice && <Notifier text={errorNotice} icon={<div className="flex h-4 w-4 items-center justify-center rounded-md border border-red-400"><FiTerminal size={9} className="text-red-400" /></div>} />}
              {runActive && (
                <ThinkingIndicator chip={chip} text={chip ? undefined : (status || 'Thinking…')} />
              )}
            </div>
          </div>
        </div>
        <div className="shrink-0">
          <ChatInput onSend={handleSend} busy={send.isPending} />
        </div>
      </div>
      <AnimatePresence>
        {rightOpen && <RightPanel key="right-panel" onClose={() => setRightOpen(false)} onOpenArtifact={onOpenArtifact} />}
      </AnimatePresence>
    </div>
  )
}
