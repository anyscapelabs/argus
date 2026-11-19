import { Browser } from '@wailsio/runtime'
import * as AccountService from '../../../bindings/github.com/anyscapelabs/argus/Services/Account/service.js'
import type { EntitlementsView, KeyStatusResult, SaveAccountKeyResult } from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models.js'

// The backend the agent pairs with. Override with VITE_ANYSCAPE_GATEWAY_URL
// (preferred) or the deprecated VITE_ARGUS_API_URL when pointing at a
// non-default Anyscape Gateway deployment.
export const ARGUS_API_URL: string =
  import.meta.env.VITE_ANYSCAPE_GATEWAY_URL ??
  import.meta.env.VITE_ARGUS_API_URL ??
  'https://api.anyscapelabs.com'

// The web console (dashboard) origin, where billing and settings live.
export const CONSOLE_URL: string =
  import.meta.env.VITE_ANYSCAPE_CONSOLE_URL ?? 'https://console.anyscapelabs.com'

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

export async function openDashboard(path: string): Promise<void> {
  const url = `${CONSOLE_URL}${path}`.replace(/([^:])\/{2,}/g, '$1/')
  try {
    await Browser.OpenURL(url)
  } catch {
    window.open(url, '_blank')
  }
}

export type { EntitlementsView, KeyStatusResult, SaveAccountKeyResult }
