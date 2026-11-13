import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { lazy, Suspense } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useAgentProfile, ensureDefaultModel } from '../domains/models/hooks'
import { useChatMessages, useSendMessage, useSetMessageReaction } from '../domains/chat/hooks'
import { noticeText } from '../domains/permission/api'
import { usePermissionFlow } from '../domains/permission/hooks'
import { useMCPConnectFlow } from '../domains/mcp/hooks'
import { useTakeoverFlow } from '../domains/browser/hooks'
import { useRunIndicator } from '../domains/chat/runIndicator'
import { useArtifacts } from '../domains/artifacts/hooks'
import { motion, AnimatePresence } from 'framer-motion'
import { FiAlertTriangle, FiArrowDown, FiChevronRight, FiGlobe, FiLink, FiTerminal } from 'react-icons/fi'
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
import VoiceNotifier from '../components/chat/VoiceNotifier'
import VoiceIcon from '../components/chat/VoiceIcon'
import VoiceMessage from '../components/chat/VoiceMessage'
const RightPanel = lazy(() => import('../components/layout/RightPanel'))
import ArtifactCard from '../components/chat/ArtifactCard'
import { weaveTranscript } from '../lib/transcript'
import { useLiveCall, type VoiceCallSummary } from '../domains/live/useLiveCall'
import { saveVoiceCall } from '../domains/live/api'
import { useAskUserFlow } from '../domains/askUser/hooks'
import AskUserCard from '../components/chat/AskUserCard'
import FileWriteCard from '../components/chat/FileWriteCard'

type PendingSend = {
  id: number
  text: string
  modelRef: string | null
  attachments: { id: string; filename: string; kind: string; dataUrl?: string }[]
}

const USAGE_LIMIT_CODES = ['daily_limit_reached', 'weekly_limit_reached', 'monthly_limit_reached', 'credits_exhausted']

function isUsageLimitMessage(msg: any): boolean {
  return msg?.role === 'assistant' && USAGE_LIMIT_CODES.includes(msg?.error_code ?? '')
}

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
  const { takeover, takeoverState, takeoverError, takeoverBusy, decide: decideTakeover } = useTakeoverFlow()
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
  const reaction = useSetMessageReaction('default')
  const voice = useLiveCall({ agentID: 'default' })
  const askUser = useAskUserFlow('default')
  const voiceActive = voice.status !== 'idle' && voice.status !== 'ended'
  // The hook hands each call over once; identity alone prevents duplicate
  // persistence if surrounding query state causes another render.
  const savedCallRef = useRef<VoiceCallSummary | null>(null)
  const pendingConsumedId = useRef<number | null>(null)
  const invalidateChat = useCallback(
    (key: string[]) => void qc.invalidateQueries({ queryKey: key }),
    [qc],
  )
  const {
    status,
    chip,
    runActive,
    setRunActive,
    beginThink,
  } = useRunIndicator(invalidateChat)

  useEffect(() => {
    if (voice.status !== 'ended') return
    const call = voice.lastCall
    if (!call || call.turns.length === 0 || savedCallRef.current === call) return

    // Only a call containing user audio and no live-model answer falls back to
    // the normal text model. Any real back-and-forth, including a partial last
    // answer, remains one voice transcript card and never starts a text run.
    if (call.unanswered) {
      if (send.isPending || runActive) return
      const text = call.turns
        .filter((turn) => turn.role === 'user')
        .map((turn) => turn.text.trim())
        .filter(Boolean)
        .join(' ')
      if (!text) return
      savedCallRef.current = call
      setRunActive(true)
      beginThink()
      send.mutate(
        { text },
        {
          onError: (err: any) => {
            console.error('voice fallback send failed', err)
            setRunActive(false)
          },
        },
      )
      return
    }

    savedCallRef.current = call
    void saveVoiceCall('default', call.turns, call.seconds)
      .then(() => qc.invalidateQueries({ queryKey: ['chat', 'default', 'history'] }))
      .catch((err) => console.error('saving voice call failed', err))
  }, [voice.status, voice.lastCall, send, runActive, setRunActive, beginThink, qc])

  // Serialize sends through the async model settle: a second Enter while the
  // first is still waiting on ensureDefaultModel must not double-send.
  const sendingRef = useRef(false)
  const handleSend = async (
    text: string,
    _modelRef: string | null,
    attachments?: { id: string; filename: string; kind: string; dataUrl?: string }[],
  ) => {
    if (sendingRef.current || send.isPending || runActive) return
    sendingRef.current = true
    // Same settle as the pendingSend path: a send racing ChatInput's one-time
    // default-model write must not leave with no model configured.
    try {
      await ensureDefaultModel(qc)
    } catch {
      // the send reports its own error if the model write failed
    }
    setRunActive(true)
    beginThink()
    send.mutate(
      {
        text,
        attachmentIDs: attachments?.map((a) => a.id),
        attachments: attachments?.map((a) => ({
          id: a.id,
          filename: a.filename,
          kind: a.kind,
          dataUrl: a.dataUrl,
        })),
      },
      {
        onSettled: () => {
          sendingRef.current = false
        },
        onError: (err: any) => {
          console.error('send failed', err)
          setRunActive(false)
        },
      },
    )
  }

  // The first message must never be lost to a startup race. The consumption
  // does not wait on the mounted profile query (a boot-time error there used
  // to stall this effect forever and swallow the message); ensureDefaultModel
  // re-reads the profile fresh, settles the default model when one is truly
  // missing, and any failure still falls through to the send, whose backend
  // error is visible instead of silent.
  useEffect(() => {
    if (!pendingSend?.text) return
    if (pendingConsumedId.current === pendingSend.id) return
    if (send.isPending || runActive) return
    pendingConsumedId.current = pendingSend.id
    onPendingConsumed?.()
    const consume = async () => {
      try {
        await ensureDefaultModel(qc)
      } catch (err) {
        console.warn('default model could not be settled', err)
      }
      setRunActive(true)
      beginThink()
      send.mutate(
        {
          text: pendingSend.text,
          attachmentIDs: pendingSend.attachments.map((a) => a.id),
          attachments: pendingSend.attachments,
        },
        {
          onError: (err: any) => {
            console.error('send failed', err)
            setRunActive(false)
          },
        },
      )
    }
    void consume()
  }, [pendingSend, qc, send, runActive, beginThink, setRunActive])

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
        askUser: askUser.request,
        inFlightTs,
      }),
    [messages, noticeLines, artifacts, takeover, mcpConnect, askUser.request, inFlightTs],
  )

  const scrollRef = useRef<HTMLDivElement>(null)
  const pinnedRef = useRef(true)
  const [pinned, setPinned] = useState(true)
  const [scrolled, setScrolled] = useState(false)
  // Where the viewport sat when an older page was requested. Prepending
  // history pushes every pixel down; without this anchor the reader of an old
  // message is thrown to the bottom of the new stack.
  const prependAnchor = useRef<{ height: number; top: number } | null>(null)
  const onScroll = () => {
    const el = scrollRef.current
    if (!el) return
    const nextPinned = el.scrollHeight - el.scrollTop - el.clientHeight < 40
    pinnedRef.current = nextPinned
    setPinned(nextPinned)
    setScrolled(el.scrollTop > 0)
    if (el.scrollTop < 160 && hasNextPage && !isFetchingNextPage) {
      prependAnchor.current = { height: el.scrollHeight, top: el.scrollTop }
      fetchNextPage()
    }
  }
  useLayoutEffect(() => {
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
  useLayoutEffect(() => {
    const el = scrollRef.current
    if (!el || !pinnedRef.current) return
    el.scrollTop = el.scrollHeight
  }, [messages, permission, noticeKeyText, errorNotice, status, mcpConnect?.request_id, takeover?.request_id, askUser.request?.request_id, artifacts.length])

  // Images, markdown cards and fonts can settle after a message commits. Follow
  // those size changes only while the reader is pinned; never pull them down
  // after they deliberately scrolled up.
  useEffect(() => {
    const viewport = scrollRef.current
    const content = viewport?.firstElementChild
    if (!viewport || !content || typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver(() => {
      if (pinnedRef.current) viewport.scrollTop = viewport.scrollHeight
    })
    observer.observe(content)
    return () => observer.disconnect()
  }, [])

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
        <Toolbar scrolled={scrolled} />
        <div className="relative flex flex-1 min-h-0 flex-col">
          {/* Profile pill with the live voice card stacked just below it. */}
          <div className="absolute left-1/2 -top-10 z-30 flex -translate-x-1/2 flex-col items-center gap-2">
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
            {voiceActive && (
              <VoiceNotifier
                status={voice.status}
                time={voice.display()}
                paused={voice.paused}
                onTogglePause={() => voice.togglePause()}
                onEnd={() => voice.end()}
              />
            )}
          </div>
          {/* Extra top padding while the voice card is showing so messages start below it. */}
          <div
            ref={scrollRef}
            onScroll={onScroll}
            className={`flex-1 min-h-0 overflow-y-auto px-6 pb-24 [scrollbar-gutter:stable] transition-[padding] duration-200 ${voiceActive ? 'pt-24' : 'pt-6'}`}
          >
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
                ) : item.kind === 'voiceMsg' ? (
                  item.turns && item.turns.length > 0 ? (
                    <VoiceMessage key={item.id} seconds={item.seconds} turns={item.turns} />
                  ) : (
                    <Notifier
                      key={item.id}
                      text={`Voice message · ${String(item.seconds).padStart(2, '0')}s`}
                      icon={
                        <div className="flex h-4 w-4 items-center justify-center rounded-md border border-secondary">
                          <VoiceIcon size={9} className="text-secondary" />
                        </div>
                      }
                    />
                  )
                ) : item.kind === 'askUserCard' ? (
                  askUser.request && (
                    <AskUserCard
                      key={item.id}
                      request={askUser.request}
                      submitting={askUser.submitting}
                      error={askUser.error}
                      onAnswer={(answer) => void askUser.answer(answer)}
                    />
                  )
                ) : item.kind === 'fileWriteCard' ? (
                  <FileWriteCard key={item.id} write={item.write} />
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
                      busy={takeoverBusy}
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
                      kind={item.artifact.kind}
                      onClick={() => onOpenArtifact?.(item.artifact.id)}
                    />
                  </div>
                ) : item.msg.role === 'user' ? (
                  <div key={item.id}>
                    <UserBubble content={item.msg.text} blocks={item.msg.blocks} />
                  </div>
                ) : isUsageLimitMessage(item.msg) ? (
                  <Notifier
                    key={item.id}
                    text={item.msg.error_message}
                    icon={
                      <div className="flex h-4 w-4 items-center justify-center rounded-md border border-red-400">
                        <FiAlertTriangle size={9} className="text-red-400" />
                      </div>
                    }
                  />
                ) : (
                  <div key={item.id}>
                    <AgentBubble
                      content={item.msg.text}
                      status={item.msg.status}
                      error={item.msg.error_message}
                      reaction={item.msg.reaction ?? null}
                      reactionPending={reaction.isPending && reaction.variables?.messageID === item.msg.id}
                      onReact={(emoji) => reaction.mutate({ messageID: item.msg.id, reaction: emoji ?? '' })}
                    />
                  </div>
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
              {!isLoading && runActive && (status || chip) && (
                <div className="flex items-center gap-3">
                  <Argus state={status && status.toLowerCase().includes('typing') ? 'typing' : 'thinking'} size={40} />
                  <ThinkingIndicator chip={chip} text={chip ? undefined : status ?? undefined} />
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
          <div className="relative">
            {voice.error && !voiceActive && (
              <div className="mb-1 text-center text-xs text-red-400">{voice.error}</div>
            )}
            <ChatInput
              onSend={handleSend}
              busy={send.isPending || !!askUser.request}
              onStartVoice={() => void voice.start()}
              voiceActive={voiceActive}
            />
          </div>
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
