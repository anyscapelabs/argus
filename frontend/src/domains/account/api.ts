import { Browser } from '@wailsio/runtime'
import * as AccountService from '../../../bindings/github.com/anyscapelabs/argus/Services/Account/service.js'
import type { EntitlementsView, KeyStatusResult, SaveAccountKeyResult } from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models.js'

// The backend the agent pairs with. Override with VITE_ARGUS_API_URL when
// pointing at a non-default argus-router deployment.
export const ARGUS_API_URL: string = import.meta.env.VITE_ARGUS_API_URL ?? 'http://localhost:8080'

export async function getKeyStatus(): Promise<KeyStatusResult> {
  return AccountService.GetKeyStatus()
}

export async function saveKey(key: string): Promise<SaveAccountKeyResult> {
  return AccountService.SaveKey(key)
}

export async function signOut(): Promise<SaveAccountKeyResult> {
  return AccountService.DeleteKey()
}

export async function entitlements(): Promise<EntitlementsView> {
  return AccountService.Entitlements(ARGUS_API_URL)
}

// openDashboard sends the operator to the browser, where argus-router serves
// the web console that mints agent keys.
export async function openDashboard(path: string): Promise<void> {
  const url = `${ARGUS_API_URL}${path}`.replace(/([^:])\/{2,}/g, '$1/')
  try {
    await Browser.OpenURL(url)
  } catch {
    window.open(url, '_blank')
  }
}

export type { EntitlementsView, KeyStatusResult, SaveAccountKeyResult }
