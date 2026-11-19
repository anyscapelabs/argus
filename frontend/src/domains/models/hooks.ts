import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getChatModels, getAgentProfile, saveAgentProfile, createAgent, deleteAgent, listAgents, listMutedAgents, setAgentMuted, listPinnedAgents, setAgentPinned } from './api'

export function useChatModels() {
  return useQuery({
    queryKey: ['models', 'chat'],
    queryFn: getChatModels,
  })
}

export function useAgentProfile(id: string) {
  return useQuery({
    queryKey: ['models', 'agent-profile', id],
    queryFn: () => getAgentProfile(id),
  })
}

export function useSaveAgentProfile() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (profile: any) => saveAgentProfile(profile),
    onSuccess: (_data, profile: any) => {
      qc.invalidateQueries({ queryKey: ['models', 'agent-profile', profile?.id ?? 'default'] })
      qc.invalidateQueries({ queryKey: ['models', 'agents'] })
    },
  })
}

export function useCreateAgent() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ name, avatar }: { name: string; avatar: string }) => createAgent(name, avatar),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['models', 'agent-profile'] })
      qc.invalidateQueries({ queryKey: ['models', 'agents'] })
    },
  })
}

export function useAgents() {
  return useQuery({
    queryKey: ['models', 'agents'],
    queryFn: () => listAgents(),
  })
}

export function useDeleteAgent() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => deleteAgent(id),
    onSuccess: (_data, id) => {
      qc.invalidateQueries({ queryKey: ['models', 'agents'] })
      qc.invalidateQueries({ queryKey: ['models', 'muted-agents'] })
      qc.invalidateQueries({ queryKey: ['models', 'pinned-agents'] })
      qc.invalidateQueries({ queryKey: ['models', 'agent-profile', id] })
      qc.removeQueries({ queryKey: ['chat', id] })
    },
  })
}

export function useMutedAgents() {
  return useQuery({
    queryKey: ['models', 'muted-agents'],
    queryFn: () => listMutedAgents(),
  })
}

export function useSetAgentMuted() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, muted }: { id: string; muted: boolean }) => setAgentMuted(id, muted),
    onMutate: async ({ id, muted }) => {
      await qc.cancelQueries({ queryKey: ['models', 'muted-agents'] })
      const prev = qc.getQueryData<string[]>(['models', 'muted-agents']) ?? []
      const set = new Set(prev)
      if (muted) set.add(id)
      else set.delete(id)
      qc.setQueryData(['models', 'muted-agents'], [...set])
      return { prev }
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(['models', 'muted-agents'], ctx.prev)
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: ['models', 'muted-agents'] })
    },
  })
}

export function usePinnedAgents() {
  return useQuery({
    queryKey: ['models', 'pinned-agents'],
    queryFn: () => listPinnedAgents(),
  })
}

export function useSetAgentPinned() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, pinned }: { id: string; pinned: boolean }) => setAgentPinned(id, pinned),
    onMutate: async ({ id, pinned }) => {
      await qc.cancelQueries({ queryKey: ['models', 'pinned-agents'] })
      const prev = qc.getQueryData<string[]>(['models', 'pinned-agents']) ?? []
      const set = new Set(prev)
      if (pinned) set.add(id)
      else set.delete(id)
      qc.setQueryData(['models', 'pinned-agents'], [...set])
      return { prev }
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(['models', 'pinned-agents'], ctx.prev)
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: ['models', 'pinned-agents'] })
    },
  })
}

