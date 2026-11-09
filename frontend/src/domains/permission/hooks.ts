import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useMutation } from '@tanstack/react-query'
import { Events } from '@wailsio/runtime'
import { getNotices, noticeText, respondPermission, type PermissionDecision, type PermissionRequestView } from './api'
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

// The permission flow: the one live request, its events, and the decision
// handler the card's buttons call. The card never decides for itself — every
// button tells the backend, and a failed save surfaces as a local notice
// because no notice row exists for it in the conversation.
export function usePermissionFlow() {
  const [permission, setPermission] = useState<PermissionRequestView | null>(null)
  const [errorNotice, setErrorNotice] = useState<string | null>(null)
  const noticeLines = useNoticeLines('default')
  const noticeKeyText = noticeLines.map((n) => noticeText(n)).join('|')
  const qc = useQueryClient()
  const key = noticesKey('default')

  // Only a failure the frontend caused itself: the Go side could not be
  // reached, so there is no notice row for the conversation to keep.
  const showLocalError = (text: string, ms = 6000) => {
    setErrorNotice(text)
    setTimeout(() => setErrorNotice(null), ms)
  }

  useEffect(() => {
    const unsubs = [
      Events.On('permission:requested', (event: any) => {
        const payload = (event?.data ?? event) as PermissionRequestView
        if (!payload?.request_id) return
        setPermission(payload)
      }),
      Events.On('permission:resolved', (event: any) => {
        const payload = (event?.data ?? event) as { request_id?: string } | undefined
        qc.invalidateQueries({ queryKey: key })
        setPermission((cur) => (cur && payload?.request_id !== cur.request_id ? null : cur))
      }),
    ]
    return () => unsubs.forEach((u) => u && u())
  }, [qc, key])

  const decide = (decision: PermissionDecision) => {
    if (!permission) return
    const requestID = permission.request_id
    setPermission(null)
    respondPermission(requestID, decision).catch((err) => {
      console.error('permission respond failed', err)
      showLocalError('Could not save that decision, try again')
    })
  }

  return {
    permission,
    decide,
    errorNotice,
    showLocalError,
    noticeLines,
    noticeKeyText,
  }
}
