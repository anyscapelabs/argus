import * as ProviderService from '../../../bindings/github.com/anyscapelabs/argus/Services/Provider/service.js'
import type { ProviderView, SaveKeyResult, ActionResult } from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models.js'

export async function listProviders(): Promise<ProviderView[]> {
  const res = await ProviderService.ListProviders()
  return res ?? []
}

export async function validateAndSaveKey(providerID: string, key: string): Promise<SaveKeyResult> {
  return ProviderService.ValidateAndSaveKey(providerID, key)
}

export async function removeKey(providerID: string): Promise<ActionResult> {
  return ProviderService.RemoveKey(providerID)
}