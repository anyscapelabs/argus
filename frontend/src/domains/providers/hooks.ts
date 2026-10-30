import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { listProviders, validateAndSaveKey, removeKey } from './api'

export function useProviders() {
  return useQuery({
    queryKey: ['providers'],
    queryFn: listProviders,
  })
}

export function useValidateAndSaveKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ providerID, key }: { providerID: string; key: string }) =>
      validateAndSaveKey(providerID, key),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['providers'] })
      await qc.refetchQueries({ queryKey: ['providers'] })
    },
  })
}

export function useRemoveKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (providerID: string) => removeKey(providerID),
    onSuccess: async (_data, providerID) => {
      await qc.invalidateQueries({ queryKey: ['providers'] })
      await qc.refetchQueries({ queryKey: ['providers'] })
      await qc.invalidateQueries({ queryKey: ['models', providerID] })
    },
  })
}