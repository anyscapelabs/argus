import * as ModelsService from '../../../bindings/github.com/anyscapelabs/argus/Services/Models/service.js'
import type {
  PickerResult,
  PickerOptions,
  SelectionsResult,
  ActionResult,
} from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models.js'

export const defaultPickerOptions: PickerOptions = {
  search: '',
  include_beta: false,
  tools_only: false,
  refresh_live: false,
}

export async function getPicker(providerID: string, options: PickerOptions = defaultPickerOptions): Promise<PickerResult> {
  return ModelsService.Picker(providerID, options)
}

export async function setProviderSelection(providerID: string, refs: string[] | null): Promise<ActionResult> {
  return ModelsService.SetProviderSelection(providerID, refs)
}

export async function getProviderSelection(providerID: string): Promise<SelectionsResult> {
  return ModelsService.GetProviderSelection(providerID)
}

export async function getChatModels() {
  const res = await ModelsService.ChatModels()
  return res ?? []
}