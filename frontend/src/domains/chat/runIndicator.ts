import { useEffect, useMemo, useRef, useState } from 'react'
import { Events } from '@wailsio/runtime'
import { StatusLine } from '../../components/chat/statusLine'
import {
  TERMINAL_WAIT_AFTER_MS,
  TERMINAL_WAITING_LABEL,
  isTerminalTool,
  terminalRunningLabel,
  toolStatusLabel,
} from '../../components/chat/toolStatus'

export type RunIndicator = {
  status: string | null
  chip: { favicon: string; title: string } | null
  runActive: boolean
  runID: string | null
  setRunActive: (v: boolean) => void
  beginThink: () => void
  statusLineRef: React.RefObject<StatusLine | null>
}

const agentsKey = ['models', 'agents']

export function useRunIndicator(
  agentID: string,
  invalidate: (key: string[]) => Promise<void> | void,
): RunIndicator {
  const [status, setStatus] = useState<string | null>(null)
  const [runActive, setRunActive] = useState(false)
  const [runID, setRunID] = useState<string | null>(null)
  const statusLineRef = useRef<StatusLine | null>(null)
  const [searchSources, setSearchSources] = useState<{ host: string; title: string }[]>([])
  const [sourceIdx, setSourceIdx] = useState(0)
  const [browsing, setBrowsing] = useState<{ host: string; title: string } | null>(null)

  useEffect(() => {
    const messagesKey = ['chat', agentID, 'history']
    const usageKey = ['chat', agentID, 'usage']
    const line = new StatusLine(setStatus)
    statusLineRef.current = line
    // A command that runs for a while stops being "Running …" and becomes
    // "Waiting for output": the agent is blocked on the terminal, and the
    // indicator should say so instead of looking idle or vanishing.
    let terminalWait: ReturnType<typeof setTimeout> | null = null
    const clearTerminalWait = () => {
      if (terminalWait) clearTimeout(terminalWait)
      terminalWait = null
    }
    const unsubs = [
      Events.On('agent:activity', (event: any) => {
        const payload = (event?.data ?? event) as
          | {
              kind?: string
              command?: string
              tool?: string
              agent_id?: string
              sources?: { host?: string; title?: string }[]
              page?: { host?: string; title?: string; action?: string; tab?: number }
            }
          | undefined
        if (!payload?.kind) return
        if (payload.agent_id && payload.agent_id !== agentID) return
        const isSearch = payload.tool === 'web_search'
        const isBrowser = payload.tool === 'browser'
        if (payload.kind === 'tool_started') {
          const cmd = (payload.command ?? '').trim()
          if (payload.tool === 'web_search') {
            setSearchSources([])
            line.set('Searching the web', 'tool')
          } else if (payload.tool === 'ask_user') {
            line.set('Waiting for your answer', 'tool')
          } else if (isBrowser) {
            // The site is the interesting part and it is not known yet; the
            // tool_results event names it as soon as Chrome has answered.
            setBrowsing(null)
            line.set('Opening a page', 'tool')
          } else if (isTerminalTool(payload.tool)) {
            clearTerminalWait()
            line.set(terminalRunningLabel(cmd), 'tool')
            terminalWait = setTimeout(() => line.set(TERMINAL_WAITING_LABEL, 'tool'), TERMINAL_WAIT_AFTER_MS)
          } else {
            line.set(toolStatusLabel(payload.tool), 'tool')
          }
        } else if (payload.kind === 'tool_results' && isSearch) {
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
          clearTerminalWait()
          // A finished tool is always followed by another model turn. Going
          // blank here is what made the indicator disappear between steps;
          // hand over to Thinking… until the next tool or the reply.
          if (payload.kind === 'tool_finished') line.set('Thinking…', 'label')
          else line.clear()
        }
      }),
      // The backend announces every model turn, including the ones between
      // tool rounds that no other event covers.
      Events.On('chat:phase', (event: any) => {
        const payload = (event?.data ?? event) as { agent_id?: string; phase?: string } | undefined
        if (payload?.agent_id && payload.agent_id !== agentID) return
        if (payload?.phase !== 'thinking') return
        clearTerminalWait()
        line.set('Thinking…', 'label')
      }),
      Events.On('chat:run_started', (event: any) => {
        const payload = (event?.data ?? event) as { agent_id?: string; run_id?: string } | undefined
        if (payload?.agent_id && payload.agent_id !== agentID) return
        setRunID(payload?.run_id ?? null)
      }),
      Events.On('chat:message', (event: any) => {
        const payload = (event?.data ?? event) as { agent_id?: string } | undefined
        void invalidate(agentsKey)
        if (payload?.agent_id && payload.agent_id !== agentID) return
        setRunActive(true)
        line.set('Thinking…', 'label')
        invalidate(messagesKey)
        invalidate(usageKey)
      }),
      Events.On('model:stream', (event: any) => {
        const ev = (event?.data ?? event) as { event?: { type?: string; delta?: { kind?: string } } } | undefined
        const type = ev?.event?.type
        const deltaKind = ev?.event?.delta?.kind
        if (type === 'block_delta' && deltaKind === 'text') {
          line.set('Typing…', 'label')
        }
      }),
      Events.On('chat:typing', (event: any) => {
        const payload = (event?.data ?? event) as { agent_id?: string } | undefined
        if (payload?.agent_id && payload.agent_id !== agentID) return
        setSearchSources([])
        setBrowsing(null)
        line.set('Typing…', 'label')
      }),
      Events.On('chat:run_finished', (event: any) => {
        const payload = (event?.data ?? event) as { agent_id?: string } | undefined
        void invalidate(agentsKey)
        if (payload?.agent_id && payload.agent_id !== agentID) return
        clearTerminalWait()
        // Keep the indicator up until the final reply is in the transcript.
        // Dropping it first leaves a gap, then the bubble pops in: a flicker.
        void Promise.all([invalidate(messagesKey), invalidate(usageKey)]).then(() => {
          setSearchSources([])
          setBrowsing(null)
          line.reset()
          setRunID(null)
          setRunActive(false)
        })
      }),
    ]
    return () => {
      unsubs.forEach((u) => u && u())
      clearTerminalWait()
      line.dispose()
      if (statusLineRef.current === line) {
        statusLineRef.current = null
      }
    }
  }, [agentID, invalidate])

  useEffect(() => {
    if (searchSources.length < 2) return
    const t = setInterval(() => setSourceIdx((i) => (i + 1) % searchSources.length), 2600)
    return () => clearInterval(t)
  }, [searchSources])

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

  const beginThink = () => {
    statusLineRef.current?.set('Thinking…', 'label')
  }

  return { status, chip, runActive, runID, setRunActive, beginThink, statusLineRef }
}
