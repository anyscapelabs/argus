import * as ChatService from '../../../bindings/github.com/anyscapelabs/argus/Services/Chat/service.js'
import type { LiveConfig } from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models.js'

export type { LiveConfig }

// liveConfig fetches a ready-to-open WebSocket URL for the voice call (the
// account key is already in its query). No model is included: the router
// resolves its configured default and injects it into the setup frame.
export async function getLiveConfig(): Promise<LiveConfig> {
  const res = await ChatService.LiveConfig()
  if (res?.error) throw new Error(res.error.message)
  if (!res.config) throw new Error('Live voice is not available')
  return res.config
}

// conversationRead answers a conversation_read function call from the live
// model, returning the tool-result text the model should see.
export async function conversationRead(agentID: string, query: string): Promise<string> {
  const res = await ChatService.ConversationRead(agentID, query)
  if (res?.error) throw new Error(res.error.message)
  return res.text ?? ''
}
