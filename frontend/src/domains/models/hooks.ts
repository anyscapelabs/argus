import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getPicker, setProviderSelection, getProviderSelection, getChatModels, getAgentProfile, setAgentModel, saveAgentProfile, defaultPickerOptions } from './api'
import type { PickerOptions } from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models.js'

export function useProviderPicker(providerID: string | null, options: PickerOptions = defaultPickerOptions) {
  return useQuery({
    queryKey: ['models', 'picker', providerID, options],
    queryFn: () => getPicker(providerID as string, options),
    enabled: !!providerID,
  })
}

export function useProviderSelection(providerID: string | null) {
  return useQuery({
    queryKey: ['models', 'selection', providerID],
    queryFn: () => getProviderSelection(providerID as string),
    enabled: !!providerID,
  })
}

export function useSetProviderSelection() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ providerID, refs }: { providerID: string; refs: string[] | null }) =>
      setProviderSelection(providerID, refs),
    onSuccess: (_data, { providerID }) => {
      qc.invalidateQueries({ queryKey: ['models', 'selection', providerID] })
      qc.invalidateQueries({ queryKey: ['models', 'chat'] })
      qc.invalidateQueries({ queryKey: ['providers'] })
    },
  })
}

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

export function useSetAgentModel() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, modelRef }: { id: string; modelRef: string }) => setAgentModel(id, modelRef),
    onSuccess: (_data, { id }) => {
      qc.invalidateQueries({ queryKey: ['models', 'agent-profile', id] })
      qc.invalidateQueries({ queryKey: ['models', 'chat'] })
    },
  })
}

export function useSaveAgentProfile() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (profile: any) => saveAgentProfile(profile),
    onSuccess: (_data, profile: any) => {
      qc.invalidateQueries({ queryKey: ['models', 'agent-profile', profile?.id ?? 'default'] })
    },
  })
}