import * as PreferencesService from '../../../bindings/github.com/anyscapelabs/argus/Services/Preferences/service.js'

export type PreferencesMap = Record<string, string>

// The preference map is read once at startup and then cached. A background
// task or another window cannot change these locally, so a second full-table
// read per hook (there are several theme hooks) would be wasted bridge traffic.
let allPromise: Promise<PreferencesMap> | null = null
const dirtyKeys = new Set<string>()

// Once the user changes a key, its in-memory/cache value is newer than the
// startup snapshot. Every hook reading that key skips the snapshot so a page
// mounting later cannot restore an older choice.
export function isPreferenceDirty(key: string): boolean {
  return dirtyKeys.has(key)
}

export function markPreferenceDirty(key: string): void {
  dirtyKeys.add(key)
}

export function loadPreferences(): Promise<PreferencesMap> {
  if (allPromise) return allPromise
  const pending: Promise<PreferencesMap> = PreferencesService.All()
    .then((res) => {
      if (res.error) throw new Error(res.error.message)
      const values: PreferencesMap = {}
      for (const [key, value] of Object.entries(res.values ?? {})) {
        if (typeof value === 'string') values[key] = value
      }
      return values
    })
    .catch((err) => {
      // Allow a later attempt after a transient startup failure.
      allPromise = null
      throw err
    })
  allPromise = pending
  return pending
}

export async function savePreference(key: string, value: string): Promise<void> {
  dirtyKeys.add(key)
  const res = await PreferencesService.Set(key, value)
  if (res.error) throw new Error(res.error.message)
}
