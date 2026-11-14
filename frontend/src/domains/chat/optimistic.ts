import type { InfiniteData } from '@tanstack/react-query'
import type { GetChatResult } from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models'

export const PENDING_USER_PREFIX = 'pending-user-'

// Gives the optimistic user row the id the backend assigned it. The row keeps
// its place and its React key, so when the history refetch lands the bubble is
// updated in place instead of being unmounted and drawn again, which is what
// made every send flash. Only the newest pending row is renamed: sends are
// serialised, so there is never more than one.
export function adoptServerMessageID(
  data: InfiniteData<GetChatResult> | undefined,
  serverID: string,
): InfiniteData<GetChatResult> | undefined {
  if (!data || !serverID) return data
  for (let p = 0; p < data.pages.length; p++) {
    const messages = data.pages[p].messages ?? []
    for (let m = messages.length - 1; m >= 0; m--) {
      if (!messages[m].id.startsWith(PENDING_USER_PREFIX)) continue
      const pages = data.pages.slice()
      const renamed = messages.slice()
      renamed[m] = { ...messages[m], id: serverID }
      pages[p] = { ...data.pages[p], messages: renamed }
      return { ...data, pages }
    }
  }
  return data
}
