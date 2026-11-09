import { useEffect, useMemo, useRef, useState } from 'react'
import { Events } from '@wailsio/runtime'
import { StatusLine } from '../../components/chat/statusLine'

// What the Argus indicator shows while a run is on: a paced status label
// (Thinking/Typing/Running…), optionally a favicon chip for the site a search
// surfaced or the browser is driving. One effect owns the whole wire: run
// bookkeeping, the status line pace, and the tool events that drive the chip.
export type RunIndicator = {
  status: string | null
  chip: { favicon: string; title: string } | null
  runActive: boolean
  setRunActive: (v: boolean) => void
  // Sets the indicator to Thinking… for a send the user just made.
  beginThink: () => void
  statusLineRef: React.RefObject<StatusLine | null>
}

const messagesKey = ['chat', 'default', 'history']
const usageKey = ['chat', 'default', 'usage']

export function useRunIndicator(invalidate: (key: string[]) => void): RunIndicator {
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

  useEffect(() => {
    const line = new StatusLine(setStatus)
    statusLineRef.current = line
    const unsubs = [
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
      Events.On('chat:message', () => {
        setRunActive(true)
        line.set('Thinking…', 'label')
        invalidate(messagesKey)
        invalidate(usageKey)
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
        invalidate(messagesKey)
        invalidate(usageKey)
      }),
    ]
    return () => {
      unsubs.forEach((u) => u && u())
      line.dispose()
      if (statusLineRef.current === line) {
        statusLineRef.current = null
      }
    }
  }, [invalidate])

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

  const beginThink = () => {
    statusLineRef.current?.set('Thinking…', 'label')
  }

  return { status, chip, runActive, setRunActive, beginThink, statusLineRef }
}
