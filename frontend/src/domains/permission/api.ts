import * as PermissionService from '../../../bindings/github.com/anyscapelabs/argus/Services/Permission/service.js'
import type { ActionResult, NoticesResult } from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models.js'

export type PermissionDecision = 'allow_once' | 'allow_always' | 'deny'

// Mirrors Shared.PermissionRequestView (event payload, not in generated models)
export type PermissionRequestView = {
  request_id: string
  agent_id: string
  agent_name?: string
  tool: string
  command: string
}

// Mirrors Shared.PermissionDecisionView
export type PermissionDecisionView = {
  request_id: string
  agent_id: string
  agent_name?: string
  tool: string
  command: string
  decision: PermissionDecision
}

// Mirrors Shared.PermissionNoticeView
export type PermissionNoticeView = {
  id: string
  agent_id: string
  agent_name: string
  tool: string
  command: string
  decision: string
  created_at: number
}

export async function respondPermission(requestID: string, decision: PermissionDecision): Promise<ActionResult> {
  const res = await PermissionService.Respond(requestID, decision)
  if (res?.error) throw new Error(res.error.message)
  return res
}

export async function getNotices(agentID: string, limit = 20): Promise<NoticesResult> {
  const res = await PermissionService.Notices(agentID, limit)
  if (res?.error) throw new Error(res.error.message)
  return res
}

export function noticeText(n: Pick<PermissionNoticeView, 'agent_name' | 'decision'>): string {
  const agent = n.agent_name || 'the agent'
  switch (n.decision) {
    case 'allow_always':
      return `Allowed ${agent} to use the terminal always`
    case 'allow_once':
      return `Allowed ${agent} to use the terminal once`
    case 'deny':
      return `Denied ${agent} access to the terminal`
    default:
      return `${n.decision} ${agent} terminal`
  }
}
