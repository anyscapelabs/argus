import { useEffect, useRef, useState } from 'react'
import { Events } from '@wailsio/runtime'
import { pendingAskUsers, respondAskUser, type AskUserRequestView } from './api'

export function useAskUserFlow(agentID: string) {
  const [request, setRequest] = useState<AskUserRequestView | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string>()
  const resolved = useRef(new Set<string>())

  useEffect(() => {
    const requested = Events.On('ask_user:requested', (event: any) => {
      const payload = (event?.data ?? event) as AskUserRequestView
      if (!payload?.request_id || payload.agent_id !== agentID) return
      setRequest(payload)
      setError(undefined)
    })
    const finished = Events.On('ask_user:resolved', (event: any) => {
      const payload = (event?.data ?? event) as { request_id?: string; agent_id?: string }
      if (!payload?.request_id || payload.agent_id !== agentID) return
      resolved.current.add(payload.request_id)
      setRequest((current) => (current?.request_id === payload.request_id ? null : current))
      setSubmitting(false)
    })
    pendingAskUsers(agentID)
      .then((requests) => {
        const next = requests[0]
        if (next && !resolved.current.has(next.request_id)) setRequest(next)
      })
      .catch(() => {})
    return () => {
      requested?.()
      finished?.()
    }
  }, [agentID])

  const answer = async (value: string) => {
    if (!request || submitting) return
    setSubmitting(true)
    setError(undefined)
    try {
      await respondAskUser(request.request_id, value)
    } catch (err) {
      setError((err as Error).message)
      setSubmitting(false)
    }
  }

  return { request, submitting, error, answer }
}
