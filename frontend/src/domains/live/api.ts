import * as ChatService from '../../../bindings/github.com/anyscapelabs/argus/Services/Chat/service.js'
import type {
  LiveConfig,
  VoiceTurn,
} from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models.js'

export type { LiveConfig, VoiceTurn }

// liveConfig fetches a ready-to-open WebSocket URL for the voice call (the
// account key is already in its query). No model is included: the router
// resolves its configured default and injects it into the setup frame.
export async function getLiveConfig(agentID: string): Promise<LiveConfig> {
  const res = await ChatService.LiveConfig(agentID)
  if (res?.error) throw new Error(res.error.message)
  if (!res.config) throw new Error('Live voice is not available')
  return res.config
}

// runLiveTool relays one Gemini function call to the backend-owned discovery,
// permission and execution bridge. No tool policy or dispatch lives here.
export async function runLiveTool(
  sessionID: string,
  callID: string,
  name: string,
  args: Record<string, unknown>,
): Promise<{ output: string; isError: boolean }> {
  const res = await ChatService.RunLiveTool(sessionID, callID, name, JSON.stringify(args))
  if (res?.error) throw new Error(res.error.message)
  return { output: res.output ?? '', isError: res.is_error }
}

export async function endLiveSession(sessionID: string): Promise<void> {
  await ChatService.EndLiveSession(sessionID)
}

// saveVoiceCall persists a finished back-and-forth as one transcript card.
export async function saveVoiceCall(
  agentID: string,
  turns: VoiceTurn[],
  seconds: number,
): Promise<boolean> {
  const res = await ChatService.SaveVoiceCall(agentID, turns, seconds)
  if (res?.error) throw new Error(res.error.message)
  return res.ok
}
