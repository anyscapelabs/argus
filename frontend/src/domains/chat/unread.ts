import { create } from 'zustand'

type UnreadStore = {
  counts: Record<string, number>
  mark: (agentID: string) => void
  clear: (agentID: string) => void
}

const storageKey = 'argus-unread'

function load(): Record<string, number> {
  try {
    const raw = localStorage.getItem(storageKey)
    if (!raw) return {}
    const parsed: unknown = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return {}
    const out: Record<string, number> = {}
    for (const [id, value] of Object.entries(parsed)) {
      if (typeof value === 'number' && Number.isFinite(value) && value > 0) {
        out[id] = Math.floor(value)
      }
    }
    return out
  } catch {
    return {}
  }
}

function save(counts: Record<string, number>) {
  try {
    localStorage.setItem(storageKey, JSON.stringify(counts))
  } catch {
    return
  }
}

export const useUnreadStore = create<UnreadStore>((set) => ({
  counts: load(),
  mark: (agentID) => {
    if (!agentID) return
    set((state) => {
      const counts = { ...state.counts, [agentID]: (state.counts[agentID] ?? 0) + 1 }
      save(counts)
      return { counts }
    })
  },
  clear: (agentID) => {
    if (!agentID) return
    set((state) => {
      if (!(agentID in state.counts)) return state
      const counts = { ...state.counts }
      delete counts[agentID]
      save(counts)
      return { counts }
    })
  },
}))

export function markUnread(agentID: string) {
  useUnreadStore.getState().mark(agentID)
}

export function clearUnread(agentID: string) {
  useUnreadStore.getState().clear(agentID)
}
