import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getPicker, setProviderSelection, getProviderSelection, getChatModels, defaultPickerOptions } from './api'
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