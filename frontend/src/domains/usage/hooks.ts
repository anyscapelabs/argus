import { useQuery } from '@tanstack/react-query'
import { fetchUsageRows, type UsageRows } from './api'

// Account-wide usage reads every agent's request log, so one query hands the
// page (and the calendar) everything; the range filter happens client-side.
export function useUsageOverview() {
  return useQuery<UsageRows>({
    queryKey: ['usage', 'overview'],
    queryFn: fetchUsageRows,
    staleTime: 30_000,
  })
}
