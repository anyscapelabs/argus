import * as MCPConnectService from '../../../bindings/github.com/anyscapelabs/argus/Services/MCPConnect/service.js'
import type { ActionResult } from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models.js'

export type MCPConnectDecision = 'connect' | 'deny'

export type MCPConnectRequestView = {
  request_id: string
  agent_id: string
  agent_name?: string
  server_id: string
  server_name: string
  description?: string
  icon?: string
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
