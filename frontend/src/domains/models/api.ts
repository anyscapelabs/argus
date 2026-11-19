import * as ModelsService from '../../../bindings/github.com/anyscapelabs/argus/Services/Models/service.js'
import type {
  ActionResult,
  AgentProfile,
  AgentProfileResult,
  AgentSummary,
  CreateAgentResult,
} from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models.js'

export async function getChatModels() {
  const res = await ModelsService.ChatModels()
  return res ?? []
}

export async function getAgentProfile(id: string): Promise<AgentProfile | null> {
  const res: AgentProfileResult = await ModelsService.GetAgentProfile(id)
  if (res?.error) throw new Error(res.error.message)
  return (res?.profile as AgentProfile) ?? null
}

export async function saveAgentProfile(profile: AgentProfile): Promise<ActionResult> {
  const res = await ModelsService.SaveAgentProfile(profile)
  if (res?.error) throw new Error(res.error.message)
  return res
}

export async function createAgent(name: string, avatar: string): Promise<CreateAgentResult> {
  const res = await ModelsService.CreateAgent(name, avatar)
  if (res?.error) throw new Error(res.error.message)
  return res
}

export async function listAgents(): Promise<AgentSummary[]> {
  const res = await ModelsService.ListAgents()
  return res ?? []
}

export async function deleteAgent(id: string): Promise<ActionResult> {
  const res = await ModelsService.DeleteAgent(id)
  if (res?.error) throw new Error(res.error.message)
  return res
}

export async function listMutedAgents(): Promise<string[]> {
  const res = await ModelsService.ListMutedAgents()
  return (res ?? []) as string[]
}

export async function setAgentMuted(id: string, muted: boolean): Promise<ActionResult> {
  const res = await ModelsService.SetAgentMuted(id, muted)
  if (res?.error) throw new Error(res.error.message)
  return res
}

export async function listPinnedAgents(): Promise<string[]> {
  const res = await ModelsService.ListPinnedAgents()
  return (res ?? []) as string[]
}

export async function setAgentPinned(id: string, pinned: boolean): Promise<ActionResult> {
  const res = await ModelsService.SetAgentPinned(id, pinned)
  if (res?.error) throw new Error(res.error.message)
  return res
}