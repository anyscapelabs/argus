import * as ChatService from '../../../bindings/github.com/anyscapelabs/argus/Services/Chat/service.js'
import type { AgentStatusResult } from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models.js'

export async function agentStatus(agentID: string): Promise<AgentStatusResult> {
  return ChatService.Status(agentID)
}

export type { AgentStatusResult }
