import * as ProviderService from '../../../bindings/github.com/anyscapelabs/argus/Services/Provider/service.js'
import type { ProviderView, SaveKeyResult, ActionResult } from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models.js'

export async function listProviders(): Promise<ProviderView[]> {
  const res = await ProviderService.ListProviders()
  return res ?? []
}

export async function validateAndSaveKey(providerID: string, key: string, refs: string[] = [], baseURLOverride: string = ''): Promise<SaveKeyResult> {
  const res = await ProviderService.ValidateAndSaveKey(providerID, key, refs, baseURLOverride)
  if (res?.error) {
    throw new Error(res.error.message)
  }
  return res
}

export async function removeKey(providerID: string): Promise<ActionResult> {
  const res = await ProviderService.RemoveKey(providerID)
  if (res?.error) {
    throw new Error(res.error.message)
  }
  return res
}