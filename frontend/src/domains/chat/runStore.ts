import { create } from 'zustand'
import { StatusLine } from '../../components/chat/statusLine'

export type SearchSource = { host: string; title: string }

export type AgentRun = {
  status: string | null
  runActive: boolean
  runID: string | null
  sourceIdx: number
  searchSources: SearchSource[]
  browsing: SearchSource | null
}

export const emptyRun: AgentRun = {
  status: null,
  runActive: false,
  runID: null,
  sourceIdx: 0,
  searchSources: [],
  browsing: null,
}

type RunStore = {
  runs: Record<string, AgentRun>
  patch: (agentID: string, next: Partial<AgentRun>) => void
  reset: (agentID: string) => void
}

export const useRunStore = create<RunStore>((set) => ({
  runs: {},
  patch: (agentID, next) =>
    set((state) => ({
      runs: {
        ...state.runs,
        [agentID]: { ...(state.runs[agentID] ?? emptyRun), ...next },
      },
    })),
  reset: (agentID) =>
    set((state) => ({ runs: { ...state.runs, [agentID]: { ...emptyRun } } })),
}))

export function patchRun(agentID: string, next: Partial<AgentRun>) {
  useRunStore.getState().patch(agentID, next)
}

export function resetRun(agentID: string) {
  useRunStore.getState().reset(agentID)
}

const statusLines = new Map<string, StatusLine>()

export function statusLine(agentID: string): StatusLine {
  let line = statusLines.get(agentID)
  if (!line) {
    line = new StatusLine((text) => patchRun(agentID, { status: text }))
    statusLines.set(agentID, line)
  }
  return line
}
