import { useEffect, useMemo, useRef, useState } from 'react'
import { lazy, Suspense } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useAgentProfile } from '../domains/models/hooks'
import { useChatMessages, useSendMessage } from '../domains/chat/hooks'
import { noticeText } from '../domains/permission/api'
import { usePermissionFlow } from '../domains/permission/hooks'
import { useMCPConnectFlow } from '../domains/mcp/hooks'
import { useTakeoverFlow } from '../domains/browser/hooks'
import { useRunIndicator } from '../domains/chat/runIndicator'
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
import PermissionCard from '../components/chat/PermissionCard'
import MCPConnectCard from '../components/mcp/MCPConnectCard'
import TakeoverCard from '../components/browser/TakeoverCard'
import Notifier from '../components/chat/Notifier'
const RightPanel = lazy(() => import('../components/layout/RightPanel'))
import ArtifactCard from '../components/chat/ArtifactCard'
import { weaveTranscript } from '../lib/transcript'

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
  const {
    permission,
    decide,
    errorNotice,
    noticeLines,
    noticeKeyText,
  } = usePermissionFlow()
  const { mcpConnect, mcpState, mcpError, decide: decideMCP } = useMCPConnectFlow()
  const { takeover, takeoverState, takeoverError, decide: decideTakeover } = useTakeoverFlow()
  // The documents this agent has written. They come from the table, not from the
  // transcript, because a document outlives the turn that made it: a card woven
  // from tool output would have to be reconstructed from message text, and the
  // table already knows the title, the version and the moment it landed.
  const { data: artifacts = [] } = useArtifacts('default')
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
  const pendingConsumedId = useRef<number | null>(null)
  const {
    status,
    chip,
    runActive,
    setRunActive,
    beginThink,
  } = useRunIndicator((key) => qc.invalidateQueries({ queryKey: key }))

  const handleSend = (text: string, _modelRef: string | null) => {
    if (send.isPending || runActive) return
    setRunActive(true)
    beginThink()
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
    beginThink()
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
    [messages, artifacts, takeover, mcpConnect, inFlightTs],
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
