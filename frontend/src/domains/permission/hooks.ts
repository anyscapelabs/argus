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

// Every notice the conversation should still be showing, oldest first. The
// notifier used to have one slot, so a connector sign-in was pushed out by the
// next permission; repeats are collapsed so a retried sign-in does not stack
// the same line over and over.
export function useNoticeLines(agentID: string, limit = 5): PermissionNoticeView[] {
  const { data } = useNotices(agentID)
  const rows = (data as PermissionNoticeView[] | undefined) ?? []
  const seen = new Set<string>()
  const out: PermissionNoticeView[] = []
  for (const n of rows) {
    const text = noticeText(n)
    if (!text || seen.has(text)) continue
    seen.add(text)
    out.push(n)
    if (out.length >= limit) break
  }
  return out.reverse()
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
