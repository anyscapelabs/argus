import * as ModelsService from '../../../bindings/github.com/anyscapelabs/argus/Services/Models/service.js'
import type {
  PickerResult,
  PickerOptions,
  SelectionsResult,
  ActionResult,
  AgentProfile,
  AgentProfileResult,
  AgentSummary,
  CreateAgentResult,
} from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models.js'

export const defaultPickerOptions: PickerOptions = {
  search: '',
  include_beta: false,
  tools_only: false,
  refresh_live: false,
}

export async function getPicker(providerID: string, options: PickerOptions = defaultPickerOptions): Promise<PickerResult> {
  const res = await ModelsService.Picker(providerID, options)
  if (res?.error) throw new Error(res.error.message)
  return res
}

export async function setProviderSelection(providerID: string, refs: string[] | null): Promise<ActionResult> {
  const res = await ModelsService.SetProviderSelection(providerID, refs ?? [])
  if (res?.error) throw new Error(res.error.message)
  return res
}

export async function getProviderSelection(providerID: string): Promise<SelectionsResult> {
  const res = await ModelsService.GetProviderSelection(providerID)
  if (res?.error) throw new Error(res.error.message)
  return res
}

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

export async function setAgentModel(id: string, modelRef: string): Promise<ActionResult> {
  const res = await ModelsService.SetAgentModel(id, modelRef)
  if (res?.error) throw new Error(res.error.message)
  return res
}