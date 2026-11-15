import { useEffect, useState } from 'react'
import { Events } from '@wailsio/runtime'
import { useQueryClient } from '@tanstack/react-query'
import { respondMCPConnect, pendingMCPConnects, listIntegrations, registerMCPConnect, disconnectIntegration, listIntegrationCatalog, type MCPConnectRequestView, type MCPConnectState } from './api'
import { useQuery } from '@tanstack/react-query'

// The connector flow: the pending sign-in card(s) the backend holds, their
// events, and the decide handler the card's buttons call. The backend holds
// the request in memory and hands it back on mount, so a reload during a
// sign-in restores the card instead of stranding it mid-flow.
export function useMCPConnectFlow(agentID = 'default') {
  const [mcpConnect, setMcpConnect] = useState<MCPConnectRequestView | null>(null)
  const [mcpState, setMcpState] = useState<MCPConnectState>('requested')
  const [mcpError, setMcpError] = useState<string>()
  const qc = useQueryClient()
  const noticesKey = ['chat', agentID, 'notices']

  useEffect(() => {
    pendingMCPConnects(agentID).then((l) => {
      const v = l && l[0]
      if (!v) return
      setMcpConnect(v)
      setMcpState(v.state || 'requested')
      setMcpError(undefined)
    })
    const unsubs = [
      Events.On('mcp:connect:status', (event: any) => {
        const payload = (event?.data ?? event) as any
        if (!payload?.request_id || payload.agent_id !== agentID) return
        if (payload.state === 'requested') {
          setMcpConnect({
            request_id: payload.request_id,
            agent_id: payload.agent_id,
            server_id: payload.server_id,
            server_name: payload.server_name,
            description: payload.description,
            icon: payload.icon,
            connection_id: payload.connection_id,
            label: payload.label,
            expected_email: payload.expected_email,
            access_level: payload.access_level,
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
                  connection_id: payload.connection_id,
                  label: payload.label,
                  expected_email: payload.expected_email,
                  access_level: payload.access_level,
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
  }, [agentID, qc])

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


export function useIntegrationCatalog() {
  const query = useQuery({
    queryKey: ['mcp', 'catalog'],
    queryFn: listIntegrationCatalog,
  })
  const qc = useQueryClient()
  useEffect(() => {
    const refresh = () => {
      void qc.invalidateQueries({ queryKey: ['mcp', 'catalog'] })
    }
    const unsubs = [
      Events.On('integrations:changed', refresh),
      Events.On('mcp:connect:status', (event: any) => {
        const state = (event?.data ?? event)?.state
        if (state === 'connected' || state === 'failed' || state === 'denied') refresh()
      }),
    ]
    return () => unsubs.forEach((unsubscribe) => unsubscribe?.())
  }, [qc])
  return query
}

export function useIntegrations() {
  return useQuery({
    queryKey: ['mcp', 'integrations'],
    queryFn: listIntegrations,
  })
}

export function useIntegrationConnect() {
  const qc = useQueryClient()
  const [states, setStates] = useState<Record<string, { state: MCPConnectState; error?: string; requestId?: string }>>({})

  useEffect(() => {
    const refresh = () => {
      void qc.invalidateQueries({ queryKey: ['mcp', 'integrations'] })
    }
    const unsubs = [
      Events.On('integrations:changed', refresh),
      Events.On('mcp:connect:status', (event: any) => {
        const payload = (event?.data ?? event) as any
        if (!payload?.server_id || !payload?.request_id) return
        setStates((cur) => {
          const prev = cur[payload.server_id]
          if (prev?.requestId && prev.requestId !== payload.request_id) return cur
          return {
            ...cur,
            [payload.server_id]: { state: payload.state, error: payload.error, requestId: payload.request_id },
          }
        })
        if (payload.state === 'connected' || payload.state === 'denied' || payload.state === 'expired') {
          refresh()
        }
      }),
    ]
    return () => unsubs.forEach((unsubscribe) => unsubscribe?.())
  }, [qc])

  const connect = async (serverId: string) => {
    setStates((cur) => ({ ...cur, [serverId]: { state: 'requested', error: undefined } }))
    try {
      const result = await registerMCPConnect('default', 'Argus', serverId)
      if (result === 'connected') {
        setStates((cur) => ({ ...cur, [serverId]: { state: 'connected' } }))
        await qc.invalidateQueries({ queryKey: ['mcp', 'integrations'] })
        return
      }
      if (result === 'unknown') throw new Error('Unknown integration')

      let request = (await pendingMCPConnects('default')).find(
        (item) => item.server_id === serverId && item.state !== 'connected',
      )
      if (!request && result === 'shown') {
        await new Promise((resolve) => setTimeout(resolve, 150))
        request = (await pendingMCPConnects('default')).find(
          (item) => item.server_id === serverId && item.state !== 'connected',
        )
      }
      if (!request) throw new Error('Could not start connection')
      setStates((cur) => ({
        ...cur,
        [serverId]: { state: 'authorizing', requestId: request.request_id },
      }))
      if (request.state !== 'authorizing') await respondMCPConnect(request.request_id, 'connect')
    } catch (error) {
      setStates((cur) => ({
        ...cur,
        [serverId]: {
          state: 'failed',
          error: error instanceof Error ? error.message : 'Could not connect',
        },
      }))
    }
  }

  const clear = (serverId: string) => setStates((cur) => {
    const next = { ...cur }
    delete next[serverId]
    return next
  })

  const disconnect = async (serverId: string) => {
    try {
      await disconnectIntegration(serverId)
      clear(serverId)
      await qc.invalidateQueries({ queryKey: ['mcp', 'integrations'] })
    } catch (error) {
      setStates((cur) => ({
        ...cur,
        [serverId]: {
          state: 'failed',
          error: error instanceof Error ? error.message : 'Could not remove connector',
        },
      }))
    }
  }

  return { states, connect, clear, disconnect }
}
