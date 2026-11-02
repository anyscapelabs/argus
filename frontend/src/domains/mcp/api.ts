import * as MCPConnectService from '../../../bindings/github.com/anyscapelabs/argus/Services/MCPConnect/service.js'
import type { ActionResult } from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models.js'

export type MCPConnectDecision = 'connect' | 'deny' | 'cancel'

export type MCPConnectState = 'requested' | 'authorizing' | 'connected' | 'failed' | 'denied' | 'expired'

export type MCPConnectRequestView = {
  request_id: string
  agent_id: string
  agent_name?: string
  server_id: string
  server_name: string
  description?: string
  icon?: string
  // Set once the card has settled, so a restored card shows its last state
  // instead of offering Connect again.
  state?: MCPConnectState
}

export type MCPConnectStatus = {
  request_id: string
  agent_id: string
  server_id: string
  server_name: string
  state: MCPConnectState
  error?: string
  tool_count?: number
}

export type MCPConnectDecisionView = {
  request_id: string
  agent_id: string
  decision: MCPConnectDecision
  server_id: string
}

export async function respondMCPConnect(requestID: string, decision: MCPConnectDecision): Promise<ActionResult> {
  const res = await MCPConnectService.Respond(requestID, decision)
  if (res?.error) throw new Error(res.error.message)
  return res
}

export async function pendingMCPConnects(agentID: string): Promise<MCPConnectRequestView[]> {
  const res = await (MCPConnectService as any).Pending(agentID)
  return res ?? []
}
