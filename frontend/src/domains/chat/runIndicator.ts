import { useCallback, useEffect, useMemo } from 'react'
import { Events } from '@wailsio/runtime'
import {
  TERMINAL_WAIT_AFTER_MS,
  TERMINAL_WAITING_LABEL,
  isTerminalTool,
  terminalRunningLabel,
  toolStatusLabel,
} from '../../components/chat/toolStatus'
import { agentStatus } from '../status/api'
import { emptyRun, patchRun, resetRun, statusLine, useRunStore } from './runStore'

export type RunIndicator = {
  status: string | null
  chip: { favicon: string; title: string } | null
  runActive: boolean
  runID: string | null
  setRunActive: (v: boolean) => void
  beginThink: () => void
}

const agentsKey = ['models', 'agents']

export function useRunIndicator(
  agentID: string,
  invalidate: (key: string[]) => Promise<void> | void,
): RunIndicator {
  const run = useRunStore((s) => s.runs[agentID] ?? emptyRun)

  useEffect(() => {
    const messagesKey = ['chat', agentID, 'history']
    const usageKey = ['chat', agentID, 'usage']
    const line = statusLine(agentID)
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
            patchRun(agentID, { searchSources: [], sourceIdx: 0 })
            line.set('Searching the web', 'tool')
          } else if (payload.tool === 'ask_user') {
            line.set('Waiting for your answer', 'tool')
          } else if (isBrowser) {
            // The site is the interesting part and it is not known yet; the
            // tool_results event names it as soon as Chrome has answered.
            patchRun(agentID, { browsing: null })
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
          patchRun(agentID, { searchSources: sites, sourceIdx: 0 })
        } else if (isBrowser && payload.kind === 'tool_results') {
          const page = payload.page
          if (page?.host) {
            patchRun(agentID, { browsing: { host: page.host, title: page.title ?? '' } })
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
        if (payload?.agent_id && payload.agent_id !== agentID) {
          if (payload.run_id) patchRun(payload.agent_id, { runID: payload.run_id, runActive: true })
          return
        }
        patchRun(agentID, { runID: payload?.run_id ?? null })
      }),
      Events.On('chat:message', (event: any) => {
        const payload = (event?.data ?? event) as { agent_id?: string } | undefined
        void invalidate(agentsKey)
        if (payload?.agent_id && payload.agent_id !== agentID) {
          patchRun(payload.agent_id, { runActive: true })
          return
        }
        patchRun(agentID, { runActive: true })
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
        patchRun(agentID, { searchSources: [], browsing: null })
        line.set('Typing…', 'label')
      }),
      Events.On('chat:run_finished', (event: any) => {
        const payload = (event?.data ?? event) as { agent_id?: string } | undefined
        void invalidate(agentsKey)
        if (payload?.agent_id && payload.agent_id !== agentID) {
          resetRun(payload.agent_id)
          statusLine(payload.agent_id).reset()
          return
        }
        clearTerminalWait()
        // Keep the indicator up until the final reply is in the transcript.
        // Dropping it first leaves a gap, then the bubble pops in: a flicker.
        void Promise.all([invalidate(messagesKey), invalidate(usageKey)]).then(() => {
          patchRun(agentID, { searchSources: [], browsing: null, runID: null, runActive: false })
          line.reset()
        })
      }),
    ]
    return () => {
      unsubs.forEach((u) => u && u())
      clearTerminalWait()
      line.dispose()
    }
  }, [agentID, invalidate])

  useEffect(() => {
    const line = statusLine(agentID)
    line.reset()
    resetRun(agentID)
    let cancelled = false
    void agentStatus(agentID)
      .then((res) => {
        if (cancelled || res.error || !res.status.busy) return
        patchRun(agentID, { runID: res.status.run_id ?? null, runActive: true })
        line.set('Thinking…', 'label')
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [agentID])

  useEffect(() => {
    if (run.searchSources.length < 2) return
    const t = setInterval(
      () => patchRun(agentID, { sourceIdx: (run.sourceIdx + 1) % run.searchSources.length }),
      2600,
    )
    return () => clearInterval(t)
  }, [agentID, run.searchSources.length, run.sourceIdx])

  const chip = useMemo(() => {
    if (run.searchSources.length > 0) {
      const s = run.searchSources[run.sourceIdx % run.searchSources.length]
      return { favicon: `https://icons.duckduckgo.com/ip3/${s.host}.ico`, title: s.title }
    }
    if (run.browsing) {
      return {
        favicon: `https://icons.duckduckgo.com/ip3/${run.browsing.host}.ico`,
        title: run.browsing.title || run.browsing.host,
      }
    }
    return null
  }, [run.searchSources, run.sourceIdx, run.browsing])

  const setRunActive = useCallback(
    (v: boolean) => patchRun(agentID, { runActive: v }),
    [agentID],
  )
  const beginThink = useCallback(
    () => statusLine(agentID).set('Thinking…', 'label'),
    [agentID],
  )

  return { status: run.status, chip, runActive: run.runActive, runID: run.runID, setRunActive, beginThink }
}
