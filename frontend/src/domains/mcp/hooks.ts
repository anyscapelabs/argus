import { useEffect, useState } from 'react'
import { Events } from '@wailsio/runtime'
import { useQueryClient } from '@tanstack/react-query'
import { respondMCPConnect, pendingMCPConnects, type MCPConnectRequestView, type MCPConnectState } from './api'

// The connector flow: the pending sign-in card(s) the backend holds, their
// events, and the decide handler the card's buttons call. The backend holds
// the request in memory and hands it back on mount, so a reload during a
// sign-in restores the card instead of stranding it mid-flow.
export function useMCPConnectFlow() {
  const [mcpConnect, setMcpConnect] = useState<MCPConnectRequestView | null>(null)
  const [mcpState, setMcpState] = useState<MCPConnectState>('requested')
  const [mcpError, setMcpError] = useState<string>()
  const qc = useQueryClient()
  const noticesKey = ['chat', 'default', 'notices']

  useEffect(() => {
    pendingMCPConnects('default').then((l) => {
      const v = l && l[0]
      if (!v) return
      setMcpConnect(v)
      setMcpState(v.state || 'requested')
      setMcpError(undefined)
    })
    const unsubs = [
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
    ]
    return () => unsubs.forEach((u) => u && u())
  }, [qc])

  const decide = (decision: 'connect' | 'deny' | 'cancel') => {
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

  return { mcpConnect, mcpState, mcpError, decide }
}
