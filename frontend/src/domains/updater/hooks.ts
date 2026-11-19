import { useEffect } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Events } from '@wailsio/runtime'
import {
  FALLBACK_VERSION,
  checkForUpdate,
  getUpdateStatus,
  installUpdate,
  type Status,
} from './api'

const STATUS_KEY = ['updater', 'status'] as const

// The page reads the backend's cached status — the background checker keeps it
// warm — and refreshes when the checker finds something, so an update that
// lands while the page is open flips it to the Update button without polling.
export function useUpdateStatus() {
  const qc = useQueryClient()
  useEffect(() => {
    const refresh = () => {
      void qc.invalidateQueries({ queryKey: STATUS_KEY })
    }
    const unsubscribe = Events.On('updates:available', refresh)
    return () => unsubscribe()
  }, [qc])

  return useQuery({
    queryKey: STATUS_KEY,
    queryFn: getUpdateStatus,
    staleTime: 30_000,
  })
}

export function useCheckForUpdate() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: checkForUpdate,
    onSuccess: (status) => qc.setQueryData(STATUS_KEY, status),
  })
}

export function useInstallUpdate() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: installUpdate,
    onSuccess: (res) => qc.setQueryData(STATUS_KEY, res.status),
  })
}

// The running version for labels that need nothing else (settings sidebar).
export function useAppVersion(): string {
  const { data } = useUpdateStatus()
  return data?.current || FALLBACK_VERSION
}

export type { Status }
