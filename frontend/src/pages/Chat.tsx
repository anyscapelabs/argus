import { useEffect, useMemo, useRef, useState } from 'react'
import { lazy, Suspense } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Events } from '@wailsio/runtime'
import { useAgentProfile } from '../domains/models/hooks'
import { useChatMessages, useSendMessage } from '../domains/chat/hooks'
import { respondPermission, noticeText, type PermissionRequestView, type PermissionDecision } from '../domains/permission/api'
import { respondMCPConnect, pendingMCPConnects, type MCPConnectRequestView, type MCPConnectState } from '../domains/mcp/api'
import { asTakeoverState, pendingTakeovers, respondTakeover, type TakeoverDecision, type TakeoverRequestView, type TakeoverState, type TakeoverStatus } from '../domains/browser/api'
import { useNoticeLines } from '../domains/permission/hooks'
import { useArtifacts } from '../domains/artifacts/hooks'
import { motion, AnimatePresence } from 'framer-motion'
import { FiArrowDown, FiChevronRight, FiGlobe, FiLink, FiTerminal } from 'react-icons/fi'
import Toolbar from '../components/layout/Toolbar'
import ChatInput from '../components/chat/ChatInput'
import AgentBubble from '../components/chat/AgentBubble'
import UserBubble from '../components/chat/UserBubble'
import ThinkingIndicator from '../components/chat/ThinkingIndicator'
import Argus from '../components/chat/Argus'
import Spinner from '../components/Spinner'
import { StatusLine } from '../components/chat/statusLine'
import PermissionCard, { type Permission } from '../components/chat/PermissionCard'
import MCPConnectCard from '../components/mcp/MCPConnectCard'
import TakeoverCard from '../components/browser/TakeoverCard'
import Notifier from '../components/chat/Notifier'
const RightPanel = lazy(() => import('../components/layout/RightPanel'))
import ArtifactCard from '../components/chat/ArtifactCard'
import { weaveTranscript } from '../lib/transcript'

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

  const transcript = useMemo(
    () =>
      weaveTranscript({
        messages,
        noticeLines,
        artifacts,
        takeover,
        mcpConnect,
        inFlightTs,
      }),
    [messages, noticeLines, artifacts, takeover, mcpConnect, inFlightTs],
  )

  const scrollRef = useRef<HTMLDivElement>(null)
  const pinnedRef = useRef(true)
  const [pinned, setPinned] = useState(true)
  // Where the viewport sat when an older page was requested. Prepending
  // history pushes every pixel down; without this anchor the reader of an old
  // message is thrown to the bottom of the new stack.
  const prependAnchor = useRef<{ height: number; top: number } | null>(null)
  const onScroll = () => {
    const el = scrollRef.current
    if (!el) return
    const nextPinned = el.scrollHeight - el.scrollTop - el.clientHeight < 120
    pinnedRef.current = nextPinned
    setPinned(nextPinned)
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

  const scrollToBottom = () => {
    const el = scrollRef.current
    if (!el) return
    pinnedRef.current = true
    setPinned(true)
    el.scrollTop = el.scrollHeight
  }

  if (isLoading) {
    return (
      <div className="flex flex-1 items-center justify-center bg-primary">
        <Spinner />
      </div>
    )
  }

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
              {!isLoading && messagesError && <div className="text-xs text-red-400">{String(messagesError)}</div>}
              {!isLoading && send.isError && (
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
                  <motion.div key={item.id} initial={{ opacity: 0, y: 6 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.25, ease: 'easeOut' }}>
                    <UserBubble content={item.msg.text} />
                  </motion.div>
                ) : (
                  <motion.div key={item.id} initial={{ opacity: 0, y: 6 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.3, ease: 'easeOut' }}>
                    <AgentBubble content={item.msg.text} status={item.msg.status} error={item.msg.error_message} />
                  </motion.div>
                ),
              )}
              {!isLoading && permission && (
                <PermissionCard
                  permission={permission}
                  agentName={agentProfile?.agent_name}
                  onAllowAlways={() => decide('allow_always')}
                  onAllowOnce={() => decide('allow_once')}
                  onDeny={() => decide('deny')}
                />
              )}
              {!isLoading && errorNotice && <Notifier text={errorNotice} icon={<div className="flex h-4 w-4 items-center justify-center rounded-md border border-red-400"><FiTerminal size={9} className="text-red-400" /></div>} />}
              {!isLoading && runActive && (
                <div className="flex items-center gap-3">
                  <Argus state={status && status.toLowerCase().includes('typing') ? 'typing' : 'thinking'} size={40} />
                  <ThinkingIndicator chip={chip} text={chip ? undefined : (status || 'Thinking…')} />
                </div>
              )}
            </div>
          </div>
          {!pinned && (
            <button
              type="button"
              onClick={scrollToBottom}
              aria-label="Scroll to bottom"
              className="absolute bottom-4 left-1/2 z-10 flex h-9 w-9 -translate-x-1/2 items-center justify-center rounded-full border border-secondary bg-secondary text-primary shadow-md transition-colors hover:bg-hover-secondary"
            >
              <FiArrowDown size={16} />
            </button>
          )}
        </div>
        <div className="shrink-0">
          <ChatInput onSend={handleSend} busy={send.isPending} />
        </div>
      </div>
      <AnimatePresence>
        {rightOpen && (
          <Suspense fallback={null}>
            <RightPanel key="right-panel" onClose={() => setRightOpen(false)} onOpenArtifact={onOpenArtifact} />
          </Suspense>
        )}
      </AnimatePresence>
    </div>
  )
}
