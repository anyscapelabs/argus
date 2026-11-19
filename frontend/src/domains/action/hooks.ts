import { useEffect, useRef, useState } from 'react'
import { Events } from '@wailsio/runtime'

// Mirrors Shared.ActionEventView (event payload, not in generated models)
export type ActionEventView = {
  action_id: string
  request_id?: string
  agent_id: string
  agent_name?: string
  kind: string
  provider: string
  status: string
  to: string[]
  cc?: string[]
  subject: string
  body: string
  from?: string
  error_text?: string
  message_id?: string
  reconciled?: string
}

// The card's own vocabulary; backend states map onto it below.
export type EmailActionStatus = 'pending' | 'sending' | 'sent' | 'denied' | 'failed' | 'unknown'

const TERMINAL: EmailActionStatus[] = ['sent', 'denied', 'failed', 'unknown']

export function isTerminal(status: EmailActionStatus): boolean {
  return TERMINAL.includes(status)
}

export function mapActionStatus(status: string): EmailActionStatus {
  switch (status) {
    case 'requested':
      return 'pending'
    case 'running':
      return 'sending'
    case 'succeeded':
      return 'sent'
    case 'denied':
      return 'denied'
    case 'failed':
      return 'failed'
    default:
      return 'unknown'
  }
}

// The email action flow: the live send an agent asked for plus its later
// status transitions. Approval itself rides the permission events.
export function useEmailActionFlow(agentID = 'default') {
  const [action, setAction] = useState<ActionEventView | null>(null)
  const [status, setStatus] = useState<EmailActionStatus>('pending')
  const [errorText, setErrorText] = useState<string | null>(null)
  const actionRef = useRef<ActionEventView | null>(null)
  actionRef.current = action

  useEffect(() => {
    const unsubs = [
      Events.On('action:requested', (event: any) => {
        const payload = (event?.data ?? event) as ActionEventView
        if (!payload?.action_id || payload.kind !== 'email_send' || payload.agent_id !== agentID) return
        actionRef.current = payload
        setAction(payload)
        setStatus(mapActionStatus(payload.status))
        setErrorText(null)
      }),
      Events.On('action:status', (event: any) => {
        const payload = (event?.data ?? event) as { action_id?: string; status?: string; error?: string }
        if (!payload?.action_id || payload.action_id !== actionRef.current?.action_id) return
        const next = mapActionStatus(payload.status ?? '')
        setStatus(next)
        setErrorText(payload.error || null)
      }),
      // A new run resets the card; the outcome is already in the notice lines.
      Events.On('chat:run_started', (event: any) => {
        const payload = (event?.data ?? event) as { agent_id?: string }
        if (payload?.agent_id && payload.agent_id !== agentID) return
        setAction(null)
        setStatus('pending')
        setErrorText(null)
      }),
    ]
    return () => unsubs.forEach((u) => u && u())
  }, [agentID])

  return { action, status, errorText, resolved: isTerminal(status) }
}
