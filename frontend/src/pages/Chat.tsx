import { useEffect, useLayoutEffect, useMemo, useRef, useState, type ComponentType } from 'react'
import { lazy, Suspense } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useAgentProfile } from '../domains/models/hooks'
import { useChatMessages, useSendMessage, useSetMessageReaction, useStopRun } from '../domains/chat/hooks'
import { noticeText } from '../domains/permission/api'
import { usePermissionFlow } from '../domains/permission/hooks'
import { useEmailActionFlow } from '../domains/action/hooks'
import { useMCPConnectFlow } from '../domains/mcp/hooks'
import { useTakeoverFlow } from '../domains/browser/hooks'
import { useRunIndicator } from '../domains/chat/runIndicator'
import { createCoalescedInvalidator } from '../domains/chat/invalidate'
import { useArtifacts } from '../domains/artifacts/hooks'
import { AnimatePresence } from 'framer-motion'
import { FiArrowDown, FiGlobe, FiLink, FiTerminal } from 'react-icons/fi'
import Toolbar from '../components/layout/Toolbar'
import ChatInput from '../components/chat/ChatInput'
import AgentBubble from '../components/chat/AgentBubble'
import UserBubble from '../components/chat/UserBubble'
import ThinkingIndicator from '../components/chat/ThinkingIndicator'
import Argus from '../components/chat/Argus'
import Spinner from '../components/Spinner'
import PermissionCard from '../components/chat/PermissionCard'
import EmailDraftCard from '../components/chat/EmailDraftCard'
import MCPConnectCard from '../components/mcp/MCPConnectCard'
import TakeoverCard from '../components/browser/TakeoverCard'
import Notifier from '../components/chat/Notifier'
import ErrorNotifier from '../components/chat/ErrorNotifier'
import VoiceNotifier from '../components/chat/VoiceNotifier'
import VoiceIcon from '../components/chat/VoiceIcon'
import VoiceMessage from '../components/chat/VoiceMessage'
const RightPanel = lazy(() => import('../components/layout/RightPanel'))
import ArtifactCard from '../components/chat/ArtifactCard'
import { weaveTranscript } from '../lib/transcript'
import { useLiveCall, type VoiceCallSummary } from '../domains/live/useLiveCall'
import { saveVoiceCall } from '../domains/live/api'
import type { PendingAttachment } from '../domains/uploads/api'
import { useAskUserFlow } from '../domains/askUser/hooks'
import { beginFirstRun } from '../domains/chat/api'
import AskUserCard from '../components/chat/AskUserCard'
import FileWriteCard from '../components/chat/FileWriteCard'

type BadgeIcon = ComponentType<{ size?: number; className?: string }>

// The small outlined icon that leads a notice line in the transcript.
const NOTICE_ICONS: Record<string, BadgeIcon> = { mcp: FiLink, takeover: FiGlobe }

function NoticeBadge({ icon: Icon }: { icon: BadgeIcon }) {
  return (
    <div className="flex h-4 w-4 items-center justify-center rounded-md border border-secondary">
      <Icon size={9} className="text-secondary" />
    </div>
  )
}

export default function Chat({
  onOpenArtifact,
  agentID = 'default',
}: {
  onOpenArtifact?: (artifactID: string) => void
  agentID?: string
}) {
  const [rightOpen, setRightOpen] = useState(false)
  const {
    permission,
    decide,
    errorNotice,
    noticeLines,
    noticeKeyText,
  } = usePermissionFlow(agentID)
  const emailAction = useEmailActionFlow(agentID)
  const { mcpConnect, mcpState, mcpError, decide: decideMCP } = useMCPConnectFlow(agentID)
  const { takeover, takeoverState, takeoverError, takeoverBusy, decide: decideTakeover } = useTakeoverFlow(agentID)
  // The documents this agent has written. They come from the table, not from the
  // transcript, because a document outlives the turn that made it: a card woven
  // from tool output would have to be reconstructed from message text, and the
  // table already knows the title, the version and the moment it landed.
  const { data: artifacts = [] } = useArtifacts(agentID)
  const { data: agentProfile } = useAgentProfile(agentID)
  const qc = useQueryClient()
  const {
    data: messages = [],
    error: messagesError,
    isLoading,
    hasNextPage,
    isFetchingNextPage,
    fetchNextPage,
  } = useChatMessages(agentID)
  const send = useSendMessage(agentID)
  const reaction = useSetMessageReaction(agentID)
  const voice = useLiveCall({ agentID })
  const askUser = useAskUserFlow(agentID)
  const voiceActive = voice.status !== 'idle' && voice.status !== 'ended'
  // The hook hands each call over once; identity alone prevents duplicate
  // persistence if surrounding query state causes another render.
  const savedCallRef = useRef<VoiceCallSummary | null>(null)
  // Backend events arrive in bursts; one coalesced refetch per burst keeps the
  // transcript from re-rendering (and flickering) several times per message.
  const chatInvalidator = useMemo(() => createCoalescedInvalidator(qc), [qc])
  useEffect(() => () => chatInvalidator.dispose(), [chatInvalidator])
  const invalidateChat = chatInvalidator.invalidate
  // First-run onboarding: the default agent's first conversation opens with a
  // seeded greeting plus a naming question driven by the backend. The backend
  // call is idempotent, and the ref keeps re-mounts from paying for it again.
  const firstRunStartedRef = useRef(false)
  useEffect(() => {
    if (agentID !== 'default' || firstRunStartedRef.current) return
    firstRunStartedRef.current = true
    beginFirstRun(agentID)
      .then(() => invalidateChat(['chat', agentID, 'history']))
      .catch((err) => console.warn('first run could not start', err))
  }, [agentID, invalidateChat])
  const {
    status,
    chip,
    runActive,
    runID,
    setRunActive,
    beginThink,
  } = useRunIndicator(agentID, invalidateChat)
  const stopRun = useStopRun()
  const handleStop = () => {
    if (!runID || stopRun.isPending) return
    stopRun.mutate(runID, {
      onError: (err) => console.warn('stop failed', err),
    })
  }

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
    void saveVoiceCall(agentID, call.turns, call.seconds)
      .then(() => qc.invalidateQueries({ queryKey: ['chat', agentID, 'history'] }))
      .catch((err) => console.error('saving voice call failed', err))
  }, [voice.status, voice.lastCall, send, runActive, setRunActive, beginThink, qc])

  // Every turn, from the composer or a voice fallback, sends the same way. The
  // backend pins every turn to the auto router, including unattended runs.
  const startSend = (text: string, attachments: PendingAttachment[] = [], onSettled?: () => void) => {
    setRunActive(true)
    beginThink()
    send.mutate(
      { text, attachmentIDs: attachments.map((a) => a.id), attachments },
      {
        onSettled,
        onError: (err: any) => {
          console.error('send failed', err)
          setRunActive(false)
        },
      },
    )
  }

  // Serialize sends so a second Enter cannot double-send.
  const sendingRef = useRef(false)
  const handleSend = (text: string, attachments?: PendingAttachment[]) => {
    if (sendingRef.current || send.isPending || runActive) return
    sendingRef.current = true
    startSend(text, attachments, () => {
      sendingRef.current = false
    })
  }

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
  }, [messages, permission, emailAction.action, noticeKeyText, errorNotice, status, mcpConnect?.request_id, takeover?.request_id, askUser.request?.request_id, artifacts.length])

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
      <div className="relative flex flex-1 flex-col min-h-0">
        {/* The bar is transparent and overlays the transcript: the window controls
            and the agent pill float above the chat, which scrolls underneath it
            (the viewport below fades out toward its top edge). */}
        <Toolbar
          className="absolute inset-x-0 top-0"
          agentAvatar={agentProfile?.avatar}
          agentName={agentProfile?.agent_name}
          onAgentClick={() => setRightOpen((v) => !v)}
        />
        <div className="relative flex flex-1 min-h-0 flex-col">
          {voiceActive && (
            <div className="absolute left-1/2 top-14 z-30 -translate-x-1/2">
              <VoiceNotifier
                status={voice.status}
                time={voice.display()}
                paused={voice.paused}
                onTogglePause={() => voice.togglePause()}
                onEnd={() => voice.end()}
              />
            </div>
          )}
          <div
            ref={scrollRef}
            onScroll={onScroll}
            className={`fade-top flex-1 min-h-0 overflow-y-auto px-6 pb-24 [scrollbar-gutter:stable] transition-[padding] duration-200 ${voiceActive ? 'pt-24' : 'pt-14'}`}
          >
            <div className="mx-auto flex max-w-3xl flex-col gap-3">
              {!isLoading && messagesError && <ErrorNotifier text={String(messagesError)} />}
              {!isLoading && send.isError && (
                <ErrorNotifier text={String((send.error as any)?.message ?? send.error)} />
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
                    icon={<NoticeBadge icon={NOTICE_ICONS[item.notice.kind] ?? FiTerminal} />}
                  />
                ) : item.kind === 'voiceMsg' ? (
                  item.turns && item.turns.length > 0 ? (
                    <VoiceMessage key={item.id} seconds={item.seconds} turns={item.turns} />
                  ) : (
                    <Notifier
                      key={item.id}
                      text={`Voice message · ${String(item.seconds).padStart(2, '0')}s`}
                      icon={<NoticeBadge icon={VoiceIcon} />}
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
                      variant="chat"
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
                  <div key={item.id} className="w-full max-w-[450px]">
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
                ) : item.msg.error_message ? (
                  <ErrorNotifier
                    key={item.id}
                    text={item.msg.error_message}
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
              {!isLoading && permission && permission.tool === 'email_send' && emailAction.action ? (
                <EmailDraftCard
                  draft={{
                    to: emailAction.action.to,
                    cc: emailAction.action.cc,
                    subject: emailAction.action.subject,
                    body: emailAction.action.body,
                    from: emailAction.action.from,
                  }}
                  agentName={agentProfile?.agent_name}
                  status={emailAction.status}
                  outcomeText={emailAction.errorText}
                  onSend={() => decide('allow_once')}
                  onDeny={() => decide('deny')}
                />
              ) : !isLoading && permission ? (
                <PermissionCard
                  permission={permission}
                  agentName={agentProfile?.agent_name}
                  onAllowAlways={() => decide('allow_always')}
                  onAllowOnce={() => decide('allow_once')}
                  onDeny={() => decide('deny')}
                />
              ) : null}
              {!isLoading && !permission && emailAction.action && emailAction.resolved && (
                <EmailDraftCard
                  draft={{
                    to: emailAction.action.to,
                    cc: emailAction.action.cc,
                    subject: emailAction.action.subject,
                    body: emailAction.action.body,
                    from: emailAction.action.from,
                  }}
                  agentName={agentProfile?.agent_name}
                  status={emailAction.status}
                  outcomeText={emailAction.errorText}
                />
              )}
              {!isLoading && errorNotice && <ErrorNotifier text={errorNotice} icon={<FiTerminal size={18} className="shrink-0 text-danger" />} />}
              {/* Shown for the whole run. Gating it on a status string made it
                  vanish whenever the status line was between two labels. */}
              {!isLoading && runActive && (
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
        {/* Same horizontal padding as the transcript viewport above, so the
            composer lines up with the message column at every window width. */}
        <div className="shrink-0 px-6">
          <div className="relative">
            {voice.error && !voiceActive && (
              <div className="mb-1">
                <ErrorNotifier text={voice.error} />
              </div>
            )}
            <ChatInput
              agentID={agentID}
              onSend={handleSend}
              busy={send.isPending || !!askUser.request}
              onStartVoice={() => void voice.start()}
              voiceActive={voiceActive}
              runActive={runActive}
              onStop={handleStop}
            />
          </div>
        </div>
      </div>
      <AnimatePresence>
        {rightOpen && (
          <Suspense fallback={null}>
            <RightPanel key="right-panel" agentID={agentID} onClose={() => setRightOpen(false)} onOpenArtifact={onOpenArtifact} />
          </Suspense>
        )}
      </AnimatePresence>
    </div>
  )
}
