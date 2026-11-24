import * as UpdaterService from '../../../bindings/github.com/anyscapelabs/argus/Services/Updater/service.js'
import type {
  Status,
  StatusResult,
  UpdateResult,
} from '../../../bindings/github.com/anyscapelabs/argus/Services/Updater/models.js'

export type { Status, StatusResult, UpdateResult }

// Shown until the backend answers (and in builds without an updater), so the
// version label never renders empty.
export const FALLBACK_VERSION = '0.1.0'

// The backend caches the last check, so this is instant and never blocks the
// page on the network.
export async function getUpdateStatus(): Promise<Status> {
  const res = await UpdaterService.Status()
  if (res?.error) throw new Error(res.error.message)
  return res.status
}

// Forces a fresh lookup against the release feed.
export async function checkForUpdate(): Promise<Status> {
  const res = await UpdaterService.Check()
  if (res?.error) throw new Error(res.error.message)
  return res.status
}

// Downloads, verifies and stages the found release, then relaunches into it.
export async function installUpdate(): Promise<UpdateResult> {
  const res = await UpdaterService.Install()
  if (res?.error) throw new Error(res.error.message)
  return res
}
