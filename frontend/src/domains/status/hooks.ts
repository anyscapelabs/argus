import { useEffect, useState } from 'react'
import { Events } from '@wailsio/runtime'
import { agentStatus, type AgentStatusResult } from './api'

// One snapshot when the panel opens, refreshed after any model stream — the
// engine's own advance, a run, a fire — plus a fresh label-ticking "now" so
// the relative wording stays honest without a 30-second network poll.
export function useAgentStatus(agentID: string) {
  const [status, setStatus] = useState<AgentStatusResult['status'] | null>(null)
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    setNow(Date.now())
    const tick = setInterval(() => setNow(Date.now()), 30 * 1000)
    return () => clearInterval(tick)
  }, [])

  useEffect(() => {
    let cancelled = false
    const refresh = () =>
      agentStatus(agentID)
        .then((res) => {
          if (cancelled) return
          if (res?.error) throw new Error(res.error.message)
          setStatus(res.status)
        })
        .catch(() => {
          if (!cancelled) setStatus(null)
        })
    refresh()
    // A turn starting or finishing is exactly what the tabs' rows describe:
    // Busy flips at run start, Last active lands at run finish.
    const unsubs = [
      Events.On('chat:message', refresh),
      Events.On('chat:run_finished', refresh),
    ]
    return () => {
      cancelled = true
      unsubs.forEach((u) => u())
    }
  }, [agentID])

  return { status, now }
}
