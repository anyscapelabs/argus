import * as ChatService from '../../../bindings/github.com/anyscapelabs/argus/Services/Chat/service.js'
import type { AskUserRequestView } from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models.js'

export type { AskUserRequestView }

export async function pendingAskUsers(agentID: string): Promise<AskUserRequestView[]> {
  const res = await ChatService.PendingAskUser(agentID)
  if (res?.error) throw new Error(res.error.message)
  return res.requests ?? []
}

export async function respondAskUser(requestID: string, answer: string): Promise<void> {
  const res = await ChatService.RespondAskUser(requestID, answer)
  if (res?.error) throw new Error(res.error.message)
}
