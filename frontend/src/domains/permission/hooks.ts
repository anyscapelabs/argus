import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useMutation } from '@tanstack/react-query'
import { getNotices, noticeText, respondPermission, type PermissionDecision } from './api'
import type { PermissionNoticeView } from './api'

const noticesKey = (agentID: string) => ['chat', agentID, 'notices']

export function useNotices(agentID: string) {
  return useQuery({
    queryKey: noticesKey(agentID),
    queryFn: async () => {
      const res = await getNotices(agentID)
      return res.notices ?? []
    },
  })
}

// Latest persisted permission decision text; survives app restarts because
// it comes from the database.
export function useLatestNoticeText(agentID: string): string | null {
  const { data } = useNotices(agentID)
  const latest = (data as PermissionNoticeView[] | undefined)?.[0]
  return latest ? noticeText(latest) : null
}

export function useRespondPermission(agentID: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ requestID, decision }: { requestID: string; decision: PermissionDecision }) =>
      respondPermission(requestID, decision),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: noticesKey(agentID) })
    },
  })
}
