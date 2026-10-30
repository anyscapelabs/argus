import * as ChatService from '../../../bindings/github.com/anyscapelabs/argus/Services/Chat/service.js'
import type {
  SendResult,
  GetChatResult,
  UsageResult,
  ListRequestsResult,
  ChatMessage,
} from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models.js'

export async function sendMessage(agentID: string, text: string): Promise<SendResult> {
  const res = await ChatService.Send(agentID, text)
  if (res?.error) throw new Error(res.error.message)
  return res
}

export async function getChat(agentID: string, beforeSeq = 0, limit = 100): Promise<GetChatResult> {
  const res = await ChatService.GetChat(agentID, beforeSeq, limit)
  if (res?.error) throw new Error(res.error.message)
  return res
}

export async function getUsage(agentID: string): Promise<UsageResult> {
  const res = await ChatService.Usage(agentID)
  if (res?.error) throw new Error(res.error.message)
  return res
}

export async function listRequests(agentID: string, limit = 100): Promise<ListRequestsResult> {
  const res = await ChatService.ListRequests(agentID, limit)
  if (res?.error) throw new Error(res.error.message)
  return res
}

export type { ChatMessage }